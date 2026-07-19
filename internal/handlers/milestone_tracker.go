package handlers

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
	"truckapi/db"
	"truckapi/internal/chrobinson"
	"truckapi/pkg/config"

	log "github.com/sirupsen/logrus"
)

// The milestone tracker infers shipment lifecycle events for booked loads by
// geofencing the assigned truck's live GPS (platform DB, resolved via the
// booking's order_bid_id) against the load's pickup and delivery stops (taken
// from the CHRob shipment-details callback). Inferred events are pushed to
// CHRob's /v1/shipments/milestones API.
//
// Lifecycle per load: XB (acknowledged, on registration) -> X3 (arrived
// pickup, truck within radius) -> AF (departed pickup with shipment) -> X6
// position pings while in transit -> X1 (arrived delivery), then done.
//
// State is in-memory and resets on restart; re-sent early events (XB) are
// harmless duplicates to CHRob. Tunables: MILESTONE_INTERVAL_MINUTES (tick,
// default 5), MILESTONE_PING_MINUTES (X6 cadence, default 30),
// GEOFENCE_RADIUS_MILES (arrival radius, default 1).

type milestoneStage int

const (
	stageNew       milestoneStage = iota // nothing sent yet
	stageAcked                           // XB sent, waiting for truck to reach pickup
	stageAtPickup                        // X3 sent, truck at pickup
	stageInTransit                       // AF sent, en route to delivery (X6 pings)
	stageDone                            // X1 sent, tracking finished
)

type milestoneState struct {
	Stage    milestoneStage
	LastPing time.Time
}

type geoPoint struct {
	Lat float64
	Lon float64
}

type stopInfo struct {
	Point    geoPoint
	Name     string
	Address1 string
	City     string
	State    string
	Zip      string
	County   string
	Country  string
}

// milestoneAction is a decision produced by the pure state machine.
type milestoneAction struct {
	EventCode    string
	LocType      string // CHRob location.type enum: P (pickup), D (drop), X (other/in transit)
	AtStop       bool   // true: use the stop's address; false: use the truck's position
	StopIsPickup bool
	NextStage    milestoneStage
	IsPing       bool
}

// decideMilestone is the pure geofence state machine: given the current stage
// and the truck's distances to the stops, decide which (if any) milestone to
// send next. One event per tick keeps ordering sane.
func decideMilestone(stage milestoneStage, distPickupMi, distDeliveryMi, radiusMi float64, sincePing, pingEvery time.Duration) *milestoneAction {
	switch stage {
	case stageNew:
		return &milestoneAction{EventCode: "XB", LocType: "P", AtStop: true, StopIsPickup: true, NextStage: stageAcked}
	case stageAcked:
		if distPickupMi <= radiusMi {
			return &milestoneAction{EventCode: "X3", LocType: "P", AtStop: true, StopIsPickup: true, NextStage: stageAtPickup}
		}
	case stageAtPickup:
		if distPickupMi > radiusMi {
			return &milestoneAction{EventCode: "AF", LocType: "P", AtStop: true, StopIsPickup: true, NextStage: stageInTransit}
		}
	case stageInTransit:
		if distDeliveryMi <= radiusMi {
			return &milestoneAction{EventCode: "X1", LocType: "D", AtStop: true, StopIsPickup: false, NextStage: stageDone}
		}
		if sincePing >= pingEvery {
			return &milestoneAction{EventCode: "X6", LocType: "X", AtStop: false, NextStage: stageInTransit, IsPing: true}
		}
	}
	return nil
}

// haversineMiles returns the great-circle distance between two points in miles.
func haversineMiles(a, b geoPoint) float64 {
	const earthRadiusMi = 3958.8
	la1 := a.Lat * math.Pi / 180
	la2 := b.Lat * math.Pi / 180
	dLat := (b.Lat - a.Lat) * math.Pi / 180
	dLon := (b.Lon - a.Lon) * math.Pi / 180
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(la1)*math.Cos(la2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadiusMi * math.Asin(math.Sqrt(h))
}

// parseStopsFromShipmentDetails extracts the pickup and delivery stops from a
// stored CHRob shipment-details callback payload (stopType 0 = pick, 1 = drop).
func parseStopsFromShipmentDetails(raw string) (pickup, delivery *stopInfo) {
	var payload struct {
		Event struct {
			Stops []struct {
				StopType     int `json:"stopType"`
				StopLocation struct {
					Address1      string `json:"address1"`
					WarehouseName string `json:"warehouseName"`
					Location      struct {
						City        string `json:"city"`
						StateCode   string `json:"stateCode"`
						PostalCode  string `json:"postalCode"`
						County      string `json:"county"`
						CountryCode string `json:"countryCode"`
						Coordinate  struct {
							Lat float64 `json:"lat"`
							Lon float64 `json:"lon"`
						} `json:"coordinate"`
					} `json:"location"`
				} `json:"stopLocation"`
			} `json:"stops"`
		} `json:"event"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, nil
	}
	for _, s := range payload.Event.Stops {
		info := &stopInfo{
			Point:    geoPoint{Lat: s.StopLocation.Location.Coordinate.Lat, Lon: s.StopLocation.Location.Coordinate.Lon},
			Name:     s.StopLocation.WarehouseName,
			Address1: s.StopLocation.Address1,
			City:     s.StopLocation.Location.City,
			State:    s.StopLocation.Location.StateCode,
			Zip:      s.StopLocation.Location.PostalCode,
			County:   s.StopLocation.Location.County,
			Country:  s.StopLocation.Location.CountryCode,
		}
		if info.Country == "" {
			info.Country = "US"
		}
		switch s.StopType {
		case 0:
			pickup = info
		case 1:
			delivery = info
		}
	}
	return pickup, delivery
}

// stopsForLoad finds the pickup/delivery stops for a load from the stored
// shipment-details callbacks. Returns nils until the callback has arrived.
func stopsForLoad(loadNumber int) (pickup, delivery *stopInfo) {
	want := strconv.Itoa(loadNumber)
	for _, rec := range runtimeStore.listShipmentDetails() {
		if rec.LoadNumber != want {
			continue
		}
		if p, d := parseStopsFromShipmentDetails(rec.RawPayload); p != nil && d != nil {
			return p, d
		}
	}
	return nil, nil
}

func buildMilestoneUpdate(loadNumber int, carrierCode string, act *milestoneAction, pos *db.TruckPosition, pickup, delivery *stopInfo) chrobinson.MilestoneUpdate {
	update := chrobinson.MilestoneUpdate{
		TransitType: "Road",
		EventCode:   act.EventCode,
		ShipmentIdentifier: chrobinson.ShipmentIdentifier{
			ShipmentNumber: strconv.Itoa(loadNumber),
		},
		DateTime: chrobinson.DateTime{
			EventDateTime: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		},
	}

	if pos != nil && pos.DriverName != "" {
		update.Carrier = &chrobinson.Carrier{
			Name: carrierCode,
			VehicleDetail: chrobinson.VehicleDetail{
				DriverContactInformation: chrobinson.DriverContactInformation{
					DriverName: pos.DriverName,
				},
			},
		}
	}

	if act.AtStop {
		stop := delivery
		if act.StopIsPickup {
			stop = pickup
		}
		update.Location = chrobinson.MilestoneLocation{
			Type: act.LocType,
			Name: stop.Name,
			Address: chrobinson.Address{
				Address1:          stop.Address1,
				City:              stop.City,
				Latitude:          stop.Point.Lat,
				Longitude:         stop.Point.Lon,
				StateProvinceCode: stop.State,
				PostalCode:        stop.Zip,
				Country:           stop.Country,
			},
		}
	} else {
		update.Location = chrobinson.MilestoneLocation{
			Type: act.LocType,
			Name: "In Transit",
			Address: chrobinson.Address{
				Latitude:  pos.Lat,
				Longitude: pos.Lng,
				Country:   "US",
			},
		}
	}
	return update
}

func milestoneEnvMinutes(key string, def int) time.Duration {
	if v := strings.TrimSpace(config.GetEnv(key, "")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Minute
		}
	}
	return time.Duration(def) * time.Minute
}

func milestoneEnvRadius() float64 {
	if v := strings.TrimSpace(config.GetEnv("GEOFENCE_RADIUS_MILES", "")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f
		}
	}
	return 1.0
}

// milestoneTick runs one evaluation pass over all tracked bookings.
func milestoneTick(apiClient *chrobinson.APIClient, states map[int]*milestoneState) {
	radius := milestoneEnvRadius()
	pingEvery := milestoneEnvMinutes("MILESTONE_PING_MINUTES", 30)

	for _, booking := range runtimeStore.listBookings() {
		if booking.OrderBidID <= 0 {
			continue
		}
		st := states[booking.LoadNumber]
		if st == nil {
			st = &milestoneState{Stage: stageNew}
			states[booking.LoadNumber] = st
		}
		if st.Stage == stageDone {
			continue
		}

		pickup, delivery := stopsForLoad(booking.LoadNumber)
		if pickup == nil || delivery == nil {
			// Shipment-details callback hasn't arrived yet; try next tick.
			continue
		}

		pos, err := db.TruckPositionByOrderBidID(booking.OrderBidID)
		if err != nil {
			log.WithError(err).WithFields(log.Fields{
				"loadNumber": booking.LoadNumber,
				"orderBidId": booking.OrderBidID,
			}).Warn("Milestone tracker: truck position unavailable")
			continue
		}

		truck := geoPoint{Lat: pos.Lat, Lon: pos.Lng}
		act := decideMilestone(
			st.Stage,
			haversineMiles(truck, pickup.Point),
			haversineMiles(truck, delivery.Point),
			radius,
			time.Since(st.LastPing),
			pingEvery,
		)
		if act == nil {
			continue
		}

		update := buildMilestoneUpdate(booking.LoadNumber, booking.CarrierCode, act, pos, pickup, delivery)
		err = chrobinson.HandleAPICall(apiClient, func() error {
			return apiClient.UpdateMilestone(update)
		})
		if err != nil {
			// Do not advance the stage on failure — retry next tick.
			log.WithError(err).WithFields(log.Fields{
				"loadNumber": booking.LoadNumber,
				"eventCode":  act.EventCode,
			}).Error("Milestone tracker: failed to send milestone")
			continue
		}

		st.Stage = act.NextStage
		if act.IsPing {
			st.LastPing = time.Now()
		}
		log.WithFields(log.Fields{
			"loadNumber": booking.LoadNumber,
			"orderBidId": booking.OrderBidID,
			"eventCode":  act.EventCode,
			"driver":     pos.DriverName,
			"truckLat":   pos.Lat,
			"truckLng":   pos.Lng,
		}).Info("Milestone tracker: milestone sent")
	}
}

// StartMilestoneTracker launches the periodic geofence milestone loop.
func StartMilestoneTracker(apiClient *chrobinson.APIClient) {
	go func() {
		interval := milestoneEnvMinutes("MILESTONE_INTERVAL_MINUTES", 5)
		log.WithFields(log.Fields{
			"interval":     interval.String(),
			"radius_miles": milestoneEnvRadius(),
		}).Info("Milestone tracker started")
		states := make(map[int]*milestoneState)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		milestoneTick(apiClient, states)
		for range ticker.C {
			milestoneTick(apiClient, states)
		}
	}()
}
