package chrobrunner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"truckapi/db"
	"truckapi/internal/chrobinson"
	"truckapi/internal/loader"

	log "github.com/sirupsen/logrus"
)

const (
	loaderOrdersEndpoint = "https://core.hfield.net/api/v1/loader/orders"
	loaderAPIKey         = "loaderBMwuIUZKtyH8fetLykDch07dxfciUZZ8lrGqOfmVaAjnXAhcwIRIdBCyhg"
)

func ChrobSearchProcess(client *chrobinson.APIClient) error {
	locations, err := db.FetchLoaderLocations("TRUCKSTOP")
	if err != nil {
		log.WithError(err).Error("Failed to fetch locations from Loader API")
		return err
	}

	log.Infof("Fetched %d locations from Loader API", len(locations))

	for _, loc := range locations {
		lat, err := parseFloatField(loc.Latitude)
		if err != nil {
			log.WithError(err).Warnf("Skipping location with invalid latitude: %q", loc.Latitude)
			continue
		}
		lng, err := parseFloatField(loc.Longitude)
		if err != nil {
			log.WithError(err).Warnf("Skipping location with invalid longitude: %q", loc.Longitude)
			continue
		}

		fromDate := time.Now().Format("2006-01-02")
		toDate := time.Now().AddDate(0, 0, 10).Format("2006-01-02")

		searchRequest := chrobinson.AvailableShipmentSearchRequest{
			PageIndex:  0,
			PageSize:   100,
			RegionCode: "NA",
			Modes:      []string{"T", "L", "F", "B", "V", "R", "O"},
			OriginRadiusSearch: &chrobinson.RadiusSearch{
				Coordinate: chrobinson.Coordinate{Lat: lat, Lon: lng},
				Radius: chrobinson.Radius{
					Value:         250,
					UnitOfMeasure: "Standard",
				},
			},
			AvailableForPickUpByDateRange: &chrobinson.DateRange{
				Min: fromDate,
				Max: toDate,
			},
			SortCriteria: &chrobinson.SortCriteria{
				Field:     "LoadNumber",
				Direction: "ascending",
			},
		}

		var searchResponse *chrobinson.AvailableShipmentSearchResponse
		err = chrobinson.HandleAPICall(client, func() error {
			resp, err := client.SearchAvailableShipments(searchRequest)
			if err != nil {
				return err
			}
			searchResponse = resp
			return nil
		})
		if err != nil {
			log.WithError(err).Error("CHRob available shipments search failed")
			continue
		}

		for _, shipment := range searchResponse.Results {
			orderPayload := mapShipmentToLoaderOrder(shipment)

			payloadBytes, err := json.Marshal(orderPayload)
			if err != nil {
				log.WithError(err).Error("Failed to marshal order payload")
				continue
			}

			req, err := http.NewRequest(
				http.MethodPost,
				loaderOrdersEndpoint,
				bytes.NewBuffer(payloadBytes),
			)
			if err != nil {
				log.WithError(err).Error("Failed to create POST request to Loader API")
				continue
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-API-KEY", loaderAPIKey)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				log.WithError(err).Error("Failed to send POST request to Loader API")
				continue
			}
			_ = resp.Body.Close()

			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
				log.Errorf("Loader API responded with status %d for order %s", resp.StatusCode, orderPayload.OrderNumber)
			} else {
				log.Infof("✅ Successfully posted CHRob order %s to Loader API", orderPayload.OrderNumber)
			}
		}
	}

	log.Info("✅ ChrobSearchProcess completed.")
	return nil
}

func StartChrobRunner(client *chrobinson.APIClient) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		ChrobSearchProcess(client)

		for range ticker.C {
			ChrobSearchProcess(client)
		}
	}()
}

func mapShipmentToLoaderOrder(shipment chrobinson.ShipmentInfo) loader.LoaderOrder {
	pickupDate := pickBestDateTime(
		shipment.CalculatedPickUpByDateTime,
		shipment.PickUpByDate,
		shipment.ReadyBy,
		shipment.AvailableForPickUp.StartDate,
	)
	deliveryDate := pickBestDateTime(
		shipment.CalculatedDeliverByDateTime,
		shipment.DeliverBy,
		shipment.DeliveryAvailableDate,
		shipment.AvailableForPickUp.EndDate,
	)

	pickupCountry := defaultCountry(shipment.Origin.Country)
	deliveryCountry := defaultCountry(shipment.Destination.Country)

	pickupLocation := formatLocation(shipment.Origin.City, shipment.Origin.State, pickupCountry)
	deliveryLocation := formatLocation(shipment.Destination.City, shipment.Destination.State, deliveryCountry)

	length := firstNonZero(shipment.Equipment.Length.Standard, shipment.SpecializedEquipment.Length.Standard)
	width := firstNonZero(shipment.Equipment.Width.Standard, shipment.SpecializedEquipment.Width.Standard)
	height := firstNonZero(shipment.Equipment.Height.Standard, shipment.SpecializedEquipment.Height.Standard)

	carrierPay := sumLoadCosts(shipment.AvailableLoadCosts)
	carrierPayRate := 0.0
	if shipment.Distance.Miles > 0 {
		carrierPayRate = carrierPay / shipment.Distance.Miles
	}

	suggestedTruckSize, truckTypeID, originalTruckSize := mapTruckType(shipment, length)

	contactEmail := contactMethodValue(shipment.Contact, "Email")
	contactPhone := firstNonEmpty(shipment.BookingContactPhoneNumber, contactMethodValue(shipment.Contact, "Phone"))
	companyName := shipment.Contact.CompanyName

	stops := shipment.StopCount
	if stops == 0 && len(shipment.Stops) > 0 {
		stops = len(shipment.Stops)
	}

	loadType := strings.Join(shipment.Modes, ",")

	return loader.LoaderOrder{
		Source:              "CHROBINSON",
		OrderNumber:         fmt.Sprintf("CHROB-%d", shipment.LoadNumber),
		PickupLocation:      pickupLocation,
		DeliveryLocation:    deliveryLocation,
		PickupDate:          pickupDate,
		DeliveryDate:        deliveryDate,
		PickupTime:          "",
		DeliveryTime:        "",
		SuggestedTruckSize:  suggestedTruckSize,
		TruckTypeId:         truckTypeID,
		OriginalTruckSize:   originalTruckSize,
		PickupZip:           shipment.Origin.Zip,
		DeliveryZip:         shipment.Destination.Zip,
		PickupCity:          shipment.Origin.City,
		PickupState:         shipment.Origin.State,
		PickupCountry:       pickupCountry,
		PickupCountryCode:   countryCode(pickupCountry),
		PickupCountryName:   pickupCountry,
		DeliveryCity:        shipment.Destination.City,
		DeliveryState:       shipment.Destination.State,
		DeliveryCountry:     deliveryCountry,
		DeliveryCountryCode: countryCode(deliveryCountry),
		DeliveryCountryName: deliveryCountry,
		EstimatedMiles:      shipment.Distance.Miles,
		OrderTypeId:         5,
		Length:              length,
		Width:               width,
		Height:              height,
		Weight:              shipment.Weight.Pounds,
		CarrierPay:          carrierPay,
		CarrierPayRate:      carrierPayRate,
		Bond:                0,
		BondTypeID:          0,
		TruckCompanyEmail:   contactEmail,
		SpecInfo:            shipment.Comment,
		PointOfContactPhone: contactPhone,
		LoadTruckstopXML:    "",
		AirRide:             boolToInt(strings.Contains(strings.ToLower(shipment.SpecializedEquipment.Description), "air")),
		LiftGate:            boolToInt(strings.Contains(strings.ToLower(shipment.SpecializedEquipment.Description), "lift")),
		LoadType:            loadType,
		Quantity:            0,
		Stops:               stops,
		TruckCompanyName:    companyName,
	}
}

func parseFloatField(value string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(value), 64)
}

func pickBestDateTime(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) == "" {
			continue
		}
		if t, ok := parseDateTime(v); ok {
			return t.Format(time.RFC3339)
		}
		return v
	}
	return ""
}

func parseDateTime(value string) (time.Time, bool) {
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02T15:04:05Z0700",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func sumLoadCosts(costs []chrobinson.AvailableLoadCost) float64 {
	if len(costs) == 0 {
		return 0
	}
	var total float64
	for _, c := range costs {
		total += c.SourceCostPerUnit * float64(c.Units)
	}
	return total
}

func mapTruckType(shipment chrobinson.ShipmentInfo, length float64) (string, int, string) {
	original := firstNonEmpty(shipment.SpecializedEquipment.Description, shipment.SpecializedEquipment.Code)
	if original == "" {
		original = "UNKNOWN"
	}

	lower := strings.ToLower(original)
	if strings.Contains(lower, "sprinter") {
		return "SPRINTER", 3, original
	}

	if length > 26 {
		return "LARGE STRAIGHT", 2, original
	}
	if length > 0 {
		return "SMALL STRAIGHT", 1, original
	}

	return "", 0, original
}

func contactMethodValue(contact chrobinson.Contact, method string) string {
	for _, m := range contact.ContactMethods {
		if strings.EqualFold(m.Method, method) && m.Value != "" {
			return m.Value
		}
	}
	return ""
}

func formatLocation(city, state, country string) string {
	parts := []string{}
	if city != "" {
		parts = append(parts, city)
	}
	if state != "" {
		parts = append(parts, state)
	}
	if country != "" {
		parts = append(parts, country)
	}
	return strings.Join(parts, ", ")
}

func defaultCountry(country string) string {
	if strings.TrimSpace(country) == "" {
		return "USA"
	}
	return country
}

func countryCode(country string) string {
	if strings.EqualFold(country, "USA") || strings.EqualFold(country, "United States") {
		return "US"
	}
	return ""
}

func firstNonZero(values ...float64) float64 {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
