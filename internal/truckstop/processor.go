package truckstop

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"truckapi/db"
	"truckapi/internal/loader"
	"truckapi/internal/uifeed"

	log "github.com/sirupsen/logrus"
)

func TruckstopSearchProcess(client *LoadSearchClient, feed *uifeed.Store) error {
	locations, err := db.FetchLoaderLocations("TRUCKSTOP")
	if err != nil {
		log.WithError(err).Error("Failed to fetch locations from Loader API")
		return err
	}

	log.Infof("Fetched %d locations from Loader API", len(locations))
	for i, loc := range locations {
		log.WithFields(log.Fields{
			"index":   i,
			"address": loc.Address,
		}).Infof("Location %d details", i+1)
	}

	equipmentGroups := [][]string{
		// Vans
		{"V", "VA", "SV", "VLG", "VM", "VB", "V-OT", "VIV", "VV", "VBV", "VBO", "VMC", "VPU", "VUL"},

		// Sprinter / Car Hauler / Cargo Vans
		{"SPV", "VCAR", "CV", "SPVR", "CVR", "CSV"},

		// Hotshots
		{"HS", "HSO", "HSD", "HOT"},

		// Reefers
		{"FRV", "VRF", "FVR"},

		// Flatbeds
		{"FSDV", "FV", "FVVR"},
	}

	for _, loc := range locations {
		originCity := ExtractCity(loc.Address)
		originState := ExtractStateCode(loc.Address)

		fromDate := time.Now().Format("2006-01-02")
		toDate := time.Now().AddDate(0, 0, 10).Format("2006-01-02")
		pickupDates := []string{fromDate, toDate}

		for _, group := range equipmentGroups {
			equipmentTypeStr := strings.Join(group, ",")

			request := LoadSearchRequest{
				IntegrationId: client.IntegrationID,
				UserName:      client.Username,
				Password:      client.Password,
				Criteria: LoadSearchCriteria{
					OriginCity:         originCity,
					OriginState:        originState,
					OriginCountry:      "USA",
					OriginRange:        250,
					DestinationCity:    "",
					DestinationState:   "",
					DestinationCountry: "USA",
					DestinationRange:   0,
					EquipmentType:      equipmentTypeStr,
					LoadType:           All,
					HoursOld:           48,
					PageNumber:         1,
					PageSize:           100,
					PickupDates:        &pickupDates,
					SortBy:             PickUpDate,
					SortDescending:     false,
				},
			}

			// Avoid logging credentials. Log only non-sensitive request context.
			log.WithFields(log.Fields{
				"origin_city":    originCity,
				"origin_state":   originState,
				"equip_group":    equipmentTypeStr,
				"pickup_from":    fromDate,
				"pickup_to":      toDate,
				"page_size":      request.Criteria.PageSize,
				"hours_old":      request.Criteria.HoursOld,
				"integration_id": client.IntegrationID,
				"username":       client.Username,
			}).Info("Prepared LoadSearchRequest for Truckstop")

			details, rawXML, err := client.GetMultipleLoadDetails(request)
			log.Infof("✅ RAW XML length for equip group %s: %d bytes", equipmentTypeStr, len(rawXML))
			if err != nil {
				log.WithError(err).Errorf("Truckstop multi-load detail search failed for address %s and equip group %v", loc.Address, group)
				continue
			}

			log.Infof("✅ Received %d LoadDetails from Truckstop for address %s (Equip Group %v)", len(details), loc.Address, group)

			for i, load := range details {
				loadJson, _ := json.MarshalIndent(load, "", "  ")
				log.WithFields(log.Fields{
					"index": i,
					"ID":    load.ID,
				}).Infof("LoadDetail received: %s", string(loadJson))
			}

			for _, load := range details {
				mapping, ok := TruckstopEquipmentMapping[load.Equipment]
				if !ok {
					log.Warnf("Skipping load %s because equipment type %s is not mapped", load.ID, load.Equipment)
					continue
				}

				var pickupTime time.Time
				ny, _ := time.LoadLocation("America/New_York")

				if s := load.PickUpDate; s != "" {
					// Interpret date-only values as ET local time
					if t, err := time.ParseInLocation("1/2/06", s, ny); err == nil {
						pickupTime = t
					} else if t, err := time.ParseInLocation("01/02/06", s, ny); err == nil {
						pickupTime = t
					} else if t, err := time.Parse(time.RFC3339, s); err == nil {
						// RFC3339 has a zone; normalize to ET
						pickupTime = t.In(ny)
					} else {
						log.WithError(err).Warnf("Unable to parse pickup date for load %s. Using current ET time.", load.ID)
						pickupTime = time.Now().In(ny)
					}
				} else {
					pickupTime = time.Now().In(ny)
				}

				pickupDateISO := pickupTime.In(ny).Format(time.RFC3339) // e.g. 2025-10-02T18:00:00-04:00
				deliveryDateISO := pickupTime.Add(24 * time.Hour).In(ny).Format(time.RFC3339)

				miles := float64(load.Mileage)
				payment := float64(load.PaymentAmount)
				length := float64(load.Length)
				width := float64(load.Width)
				height := 1.0

				// If width not provided, try to parse from Dims
				if width == 0 && load.Dims != "" {
					if w := parseWidthFromDims(load.Dims); w > 0 {
						width = w
					}
				}

				// If height is still default (1.0), try to parse it from Dims
				if height == 1.0 && load.Dims != "" {
					if h := parseHeightFromDims(load.Dims); h > 1.0 {
						height = h
					}
				}

				weight := float64(load.Weight)

				carrierPayRate := 0.0
				if miles > 0 {
					carrierPayRate = payment / miles
				}

				// Do not default ZIPs if missing; leave as empty string
				originZip := load.OriginZip
				destinationZip := load.DestinationZip
				// Exclude zip code from pickupLocation and deliveryLocation
				pickupLocation := fmt.Sprintf("%s, %s, USA", load.OriginCity, load.OriginState)
				deliveryLocation := fmt.Sprintf("%s, %s, USA", load.DestinationCity, load.DestinationState)

				bond := int(load.Bond)
				bondTypeID := int(load.BondTypeID)

				// Marshal this individual load back to XML so we include only its data (not the entire SOAP body)
				loadXMLBytes, err := xml.Marshal(load)
				if err != nil {
					log.WithError(err).Warnf("Failed to marshal load %s back to XML", load.ID)
				}
				loadXML := string(loadXMLBytes)

				// // Extract the exact SOAP fragment for this load
				// rawFragmentXML := extractLoadDetailXML(rawXML, load.ID)

				// Insert airRide/liftGate logic
				var airRide int
				var liftGate int
				switch load.Equipment {
				case "VA", "FA":
					airRide = 1
				case "VLG":
					liftGate = 1
				}

				originalTruckSize, ok := TruckstopEquipmentNames[load.Equipment]
				if !ok {
					originalTruckSize = load.Equipment
				}

				orderPayload := loader.LoaderOrder{
					Source:              "TRUCKSTOP",
					OrderNumber:         fmt.Sprintf("TS-%s", load.ID),
					PickupLocation:      pickupLocation,
					DeliveryLocation:    deliveryLocation,
					PickupDate:          pickupDateISO,
					PickupTime:          load.PickUpTime,
					DeliveryDate:        deliveryDateISO,
					DeliveryTime:        load.DeliveryTime,
					SuggestedTruckSize:  mapping.SuggestedTruckSize,
					TruckTypeId:         mapping.TruckTypeId,
					OriginalTruckSize:   originalTruckSize,
					PickupZip:           originZip,
					DeliveryZip:         destinationZip,
					PickupCity:          load.OriginCity,
					PickupState:         load.OriginState,
					PickupCountry:       "USA",
					PickupCountryCode:   "US",
					PickupCountryName:   "United States",
					DeliveryCity:        load.DestinationCity,
					DeliveryState:       load.DestinationState,
					DeliveryCountry:     "USA",
					DeliveryCountryCode: "US",
					DeliveryCountryName: "United States",
					EstimatedMiles:      miles,
					OrderTypeId:         5,
					Length:              length,
					Width:               width,
					Height:              height,
					Weight:              weight,
					CarrierPay:          payment,
					CarrierPayRate:      carrierPayRate,
					Bond:                bond,
					BondTypeID:          bondTypeID,
					LoadTruckstopXML:    loadXML,
					TruckCompanyName:    load.TruckCompanyName,
					TruckCompanyEmail:   load.TruckCompanyEmail,
					SpecInfo:            load.SpecInfo,
					PointOfContactPhone: load.PointOfContactPhone,
					AirRide:             airRide,
					LiftGate:            liftGate,
					// Newly added fields:
					LoadType: load.LoadType,
					Quantity: int(load.Quantity),
					Stops:    int(load.Stops),
				}

				orderPayloadJson, _ := json.MarshalIndent(orderPayload, "", "  ")
				// log.WithField("LoaderOrder", string(orderPayloadJson)).Infof("Posting LoaderOrder for load %s", load.ID)
				fmt.Printf("\n=== JSON PAYLOAD SENT TO LOADER API (Load ID: %s) ===\n%s\n\n", load.ID, string(orderPayloadJson))
				fmt.Printf("\n=== RAW XML FOR LOAD ID %s ===\n%s\n\n", load.ID, orderPayload.LoadTruckstopXML)
				// fmt.Printf("\n=== RAW SOAP FRAGMENT FOR LOAD ID %s ===\n%s\n\n", load.ID, orderPayload.RawTruckstopFragmentXML)

				// Prototype: do not POST to Loader API. Send to in-memory UI feed instead.
				// Old Loader API POST is intentionally removed/commented for prototype verification.
				if feed != nil {
					feed.Add(orderPayload)
				}
			}
		}
	}

	log.Info("✅ TruckstopSearchProcess completed.")
	return nil
}

// extractLoadDetailXML pulls the <MultipleLoadDetailResult>...</MultipleLoadDetailResult>
// block corresponding to a single load ID from the raw SOAP envelope.
func extractLoadDetailXML(rawXML, loadID string) string {
	re := regexp.MustCompile(fmt.Sprintf(`(?s)<[A-Za-z0-9]*:?MultipleLoadDetailResult[^>]*>.*?<[^>]*:?ID[^>]*>%s</[^>]*:?ID>.*?</[A-Za-z0-9]*:?MultipleLoadDetailResult>`, loadID))
	return re.FindString(rawXML)
}

// parseWidthFromDims tries to extract the width (second dimension) from a Dims string
// Example Dims: "16ft8inch x 5ft3inch x 6ft1inch x 3703 lbs"
func parseWidthFromDims(dims string) float64 {
	// First, split on 'x'
	parts := strings.Split(dims, "x")
	if len(parts) < 2 {
		return 0
	}

	// Take the second part (index 1) as width fragment
	widthStr := strings.TrimSpace(parts[1])

	// regex feet and inches
	re := regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*ft\s*(\d+(?:\.\d+)?)?\s*inch`)
	m := re.FindStringSubmatch(widthStr)
	if len(m) == 0 {
		return 0
	}

	feet, _ := strconv.ParseFloat(m[1], 64)
	inches := 0.0
	if m[2] != "" {
		inches, _ = strconv.ParseFloat(m[2], 64)
	}
	return feet + inches/12.0
}

// parseHeightFromDims tries to extract the height (third dimension) from a Dims string
// Example: "16ft8inch x 5ft3inch x 6ft1inch x 3703 lbs"
func parseHeightFromDims(dims string) float64 {
	parts := strings.Split(dims, "x")
	if len(parts) < 3 {
		return 0
	}

	heightStr := strings.TrimSpace(parts[2])

	re := regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*ft\s*(\d+(?:\.\d+)?)?\s*inch`)
	m := re.FindStringSubmatch(heightStr)
	if len(m) == 0 {
		return 0
	}

	feet, _ := strconv.ParseFloat(m[1], 64)
	inches := 0.0
	if m[2] != "" {
		inches, _ = strconv.ParseFloat(m[2], 64)
	}
	return feet + inches/12.0
}

// SplitAndTrim splits a string by a separator and trims whitespace from each element.
func SplitAndTrim(s, sep string) []string {
	parts := []string{}
	for _, p := range strings.Split(s, sep) {
		part := strings.TrimSpace(p)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

// StartTruckstopRunner starts a background goroutine to run every 3 minutes.
func StartTruckstopRunner(client *LoadSearchClient, feed *uifeed.Store) {
	go func() {
		log.WithFields(log.Fields{
			"runner": "TRUCKSTOP",
		}).Info("Runner started")

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		runOnce := func() {
			start := time.Now()
			log.WithFields(log.Fields{
				"runner": "TRUCKSTOP",
			}).Info("Runner cycle start")

			if err := TruckstopSearchProcess(client, feed); err != nil {
				log.WithError(err).WithFields(log.Fields{
					"runner":      "TRUCKSTOP",
					"duration_ms": time.Since(start).Milliseconds(),
				}).Error("Runner cycle failed")
				return
			}

			log.WithFields(log.Fields{
				"runner":      "TRUCKSTOP",
				"duration_ms": time.Since(start).Milliseconds(),
			}).Info("Runner cycle complete")
		}

		// Run immediately on startup.
		runOnce()

		for range ticker.C {
			runOnce()
		}
	}()
}
