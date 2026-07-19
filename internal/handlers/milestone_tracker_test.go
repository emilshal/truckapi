package handlers

import (
	"math"
	"testing"
	"time"
	"truckapi/db"
)

func TestHaversineMiles(t *testing.T) {
	sd := geoPoint{Lat: 32.715, Lon: -117.1573}      // San Diego
	memphis := geoPoint{Lat: 35.1497, Lon: -90.0487} // Memphis

	if d := haversineMiles(sd, sd); d != 0 {
		t.Fatalf("same point should be 0, got %v", d)
	}
	// Great-circle SD->Memphis is ~1550 miles (road distance is ~1760).
	d := haversineMiles(sd, memphis)
	if math.Abs(d-1550) > 50 {
		t.Fatalf("SD->Memphis expected ~1550mi great-circle, got %v", d)
	}
}

// Full lifecycle: XB -> X3 (arrive pickup) -> AF (depart) -> X6 pings -> X1 (arrive delivery).
func TestDecideMilestone_Lifecycle(t *testing.T) {
	const radius = 1.0
	ping := 30 * time.Minute

	// New load: acknowledge regardless of position.
	act := decideMilestone(stageNew, 500, 1500, radius, 0, ping)
	if act == nil || act.EventCode != "XB" || act.NextStage != stageAcked {
		t.Fatalf("stageNew should produce XB->acked, got %+v", act)
	}

	// Far from pickup: nothing.
	if act = decideMilestone(stageAcked, 12, 1500, radius, 0, ping); act != nil {
		t.Fatalf("far from pickup should be nil, got %+v", act)
	}

	// Within radius of pickup: X3.
	act = decideMilestone(stageAcked, 0.4, 1500, radius, 0, ping)
	if act == nil || act.EventCode != "X3" || act.NextStage != stageAtPickup || !act.StopIsPickup {
		t.Fatalf("arrival at pickup should produce X3, got %+v", act)
	}

	// Still at pickup: nothing.
	if act = decideMilestone(stageAtPickup, 0.2, 1500, radius, 0, ping); act != nil {
		t.Fatalf("still at pickup should be nil, got %+v", act)
	}

	// Left pickup radius: AF departed.
	act = decideMilestone(stageAtPickup, 3.5, 1490, radius, 0, ping)
	if act == nil || act.EventCode != "AF" || act.NextStage != stageInTransit {
		t.Fatalf("departure should produce AF, got %+v", act)
	}

	// In transit, ping interval not reached: nothing.
	if act = decideMilestone(stageInTransit, 300, 900, radius, 10*time.Minute, ping); act != nil {
		t.Fatalf("in transit before ping interval should be nil, got %+v", act)
	}

	// In transit, ping due: X6 position update, stage unchanged.
	act = decideMilestone(stageInTransit, 300, 900, radius, 31*time.Minute, ping)
	if act == nil || act.EventCode != "X6" || !act.IsPing || act.NextStage != stageInTransit || act.AtStop {
		t.Fatalf("ping due should produce X6 at truck position, got %+v", act)
	}

	// Arrived at delivery: X1, done. Takes priority over pings.
	act = decideMilestone(stageInTransit, 1500, 0.7, radius, 45*time.Minute, ping)
	if act == nil || act.EventCode != "X1" || act.NextStage != stageDone || act.StopIsPickup {
		t.Fatalf("arrival at delivery should produce X1->done, got %+v", act)
	}

	// Done: never anything again.
	if act = decideMilestone(stageDone, 0, 0, radius, time.Hour, ping); act != nil {
		t.Fatalf("done stage should be nil, got %+v", act)
	}
}

func TestParseStopsFromShipmentDetails(t *testing.T) {
	// Trimmed real CHRob shipment-details payload shape.
	raw := `{"carrierCode":"T6323830","loadNumber":"202268063","event":{"eventType":"LOAD DETAIL CHANGED","eventSubType":"Shipment Booked","stops":[{"stopType":0,"stopNumber":0,"stopLocation":{"address1":"9990 Alesmith Ct","warehouseName":"QA Test Warehouse 1","location":{"city":"San Diego","stateCode":"CA","postalCode":"92126","county":"San Diego County","countryCode":"US","coordinate":{"lat":32.715,"lon":-117.1573}}}},{"stopType":1,"stopNumber":1,"stopLocation":{"address1":"2783 Broad Ave","warehouseName":"QA Test Warehouse 2","location":{"city":"Memphis","stateCode":"TN","postalCode":"38112","county":"Shelby County","countryCode":"US","coordinate":{"lat":35.1497,"lon":-90.0487}}}}]}}`

	pickup, delivery := parseStopsFromShipmentDetails(raw)
	if pickup == nil || delivery == nil {
		t.Fatalf("expected both stops parsed, got pickup=%v delivery=%v", pickup, delivery)
	}
	if pickup.City != "San Diego" || pickup.Zip != "92126" || pickup.Point.Lat != 32.715 {
		t.Fatalf("bad pickup: %+v", pickup)
	}
	if delivery.City != "Memphis" || delivery.Zip != "38112" || delivery.Point.Lon != -90.0487 {
		t.Fatalf("bad delivery: %+v", delivery)
	}
	if pickup.Name != "QA Test Warehouse 1" || delivery.Address1 != "2783 Broad Ave" {
		t.Fatalf("names/addresses not carried: %+v %+v", pickup, delivery)
	}
}

func TestParseStopsFromShipmentDetails_Garbage(t *testing.T) {
	if p, d := parseStopsFromShipmentDetails("not json"); p != nil || d != nil {
		t.Fatalf("garbage should yield nils")
	}
	if p, d := parseStopsFromShipmentDetails(`{"event":{"stops":[]}}`); p != nil || d != nil {
		t.Fatalf("no stops should yield nils")
	}
}

func TestBuildMilestoneUpdate_StopVsPing(t *testing.T) {
	pickup := &stopInfo{Point: geoPoint{32.715, -117.1573}, Name: "WH1", Address1: "9990 Alesmith Ct", City: "San Diego", State: "CA", Zip: "92126", Country: "US"}
	delivery := &stopInfo{Point: geoPoint{35.1497, -90.0487}, Name: "WH2", City: "Memphis", State: "TN", Zip: "38112", Country: "US"}
	pos := &db.TruckPosition{DriverName: "Gocha Tsibadze", Lat: 32.9120, Lng: -117.1434}

	// Stop-anchored event uses the stop's address and type.
	act := &milestoneAction{EventCode: "X3", LocType: "P", AtStop: true, StopIsPickup: true, NextStage: stageAtPickup}
	u := buildMilestoneUpdate(202268063, "T6323830", act, pos, pickup, delivery)
	if u.TransitType != "Road" || u.ShipmentIdentifier.ShipmentNumber != "202268063" {
		t.Fatalf("bad identifier: %+v", u)
	}
	if u.Location.Type != "P" || u.Location.Address.City != "San Diego" || u.Location.Address.Latitude != 32.715 {
		t.Fatalf("X3 should anchor at pickup, got %+v", u.Location)
	}
	if u.Carrier == nil || u.Carrier.VehicleDetail.DriverContactInformation.DriverName != "Gocha Tsibadze" {
		t.Fatalf("driver not carried: %+v", u.Carrier)
	}

	// Ping uses the truck's live position.
	ping := &milestoneAction{EventCode: "X6", LocType: "X", AtStop: false, NextStage: stageInTransit, IsPing: true}
	u = buildMilestoneUpdate(202268063, "T6323830", ping, pos, pickup, delivery)
	if u.Location.Type != "X" || u.Location.Address.Latitude != 32.9120 || u.Location.Name != "In Transit" {
		t.Fatalf("X6 should anchor at truck position, got %+v", u.Location)
	}
}
