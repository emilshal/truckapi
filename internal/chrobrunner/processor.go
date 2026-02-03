package chrobrunner

import (
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"time"
	"truckapi/db"
	"truckapi/internal/chrobinson"
	"truckapi/internal/loader"
	"truckapi/internal/uifeed"

	log "github.com/sirupsen/logrus"
)

func chrobDedupKey(shipment chrobinson.ShipmentInfo, order loader.LoaderOrder) string {
	// Prefer loadNumber when present.
	if shipment.LoadNumber != 0 {
		return fmt.Sprintf("loadNumber:%d", shipment.LoadNumber)
	}

	// Fallback: hash a stable subset of mapped fields.
	// This avoids infinite paging when the API ignores pageIndex and repeats page 0.
	h := fnv.New64a()
	write := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	write(order.Source)
	write(order.PickupLocation)
	write(order.DeliveryLocation)
	write(order.PickupDate)
	write(order.DeliveryDate)
	write(order.OriginalTruckSize)
	write(order.SuggestedTruckSize)
	write(order.TruckCompanyName)
	write(fmt.Sprintf("%.3f", order.EstimatedMiles))
	write(fmt.Sprintf("%.3f", order.Weight))

	sum := make([]byte, 8)
	binaryPutUint64(sum, h.Sum64())
	return "fallback:" + hex.EncodeToString(sum)
}

func binaryPutUint64(dst []byte, v uint64) {
	_ = dst[7]
	dst[0] = byte(v >> 56)
	dst[1] = byte(v >> 48)
	dst[2] = byte(v >> 40)
	dst[3] = byte(v >> 32)
	dst[4] = byte(v >> 24)
	dst[5] = byte(v >> 16)
	dst[6] = byte(v >> 8)
	dst[7] = byte(v)
}

func ChrobSearchProcess(client *chrobinson.APIClient, feed *uifeed.Store) error {
	locations, err := db.FetchLoaderLocations("TRUCKSTOP")
	if err != nil {
		log.WithError(err).Error("Failed to fetch locations from Loader API")
		return err
	}

	log.Infof("Fetched %d locations from Loader API", len(locations))

	var (
		processedLocations int
		skippedLocations   int
		searchErrors       int
		totalShipments     int
		totalEnqueued      int
		totalPosted        int
		companyNameCounts  = map[string]int{}
		originNameCounts   = map[string]int{}
		destNameCounts     = map[string]int{}
	)

	for _, loc := range locations {
		lat, err := parseFloatField(loc.Latitude)
		if err != nil {
			log.WithError(err).Warnf("Skipping location with invalid latitude: %q", loc.Latitude)
			skippedLocations++
			continue
		}
		lng, err := parseFloatField(loc.Longitude)
		if err != nil {
			log.WithError(err).Warnf("Skipping location with invalid longitude: %q", loc.Longitude)
			skippedLocations++
			continue
		}

		processedLocations++
		log.WithFields(log.Fields{
			"lat":    lat,
			"lng":    lng,
			"source": "CHROBINSON",
		}).Info("CHRob search start (location)")

		fromDate := time.Now().Format("2006-01-02")
		toDate := time.Now().AddDate(0, 0, 10).Format("2006-01-02")

		baseRequest := chrobinson.AvailableShipmentSearchRequest{
			PageIndex:  0,
			PageSize:   100,
			RegionCode: "NA",
			Modes:      []string{"F", "L", "R", "V", "H"},
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

		seenKeys := make(map[string]struct{}, 512)
		pageIndex := 0
		const maxPagesPerLocation = 50
		for {
			if pageIndex >= maxPagesPerLocation {
				log.WithFields(log.Fields{
					"lat":       lat,
					"lng":       lng,
					"pageIndex": pageIndex,
					"pageSize":  baseRequest.PageSize,
				}).Warn("CHRob paging hit max page cap; stopping pagination for location")
				break
			}

			searchRequest := baseRequest
			searchRequest.PageIndex = pageIndex

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
				log.WithError(err).WithFields(log.Fields{
					"pageIndex": pageIndex,
				}).Error("CHRob available shipments search failed")
				searchErrors++
				break
			}

			if searchResponse == nil || len(searchResponse.Results) == 0 {
				log.WithFields(log.Fields{
					"lat":       lat,
					"lng":       lng,
					"pageIndex": pageIndex,
				}).Info("CHRob search complete (no more results)")
				break
			}

			log.WithFields(log.Fields{
				"results":    len(searchResponse.Results),
				"totalCount": searchResponse.TotalCount,
				"lat":        lat,
				"lng":        lng,
				"pageIndex":  pageIndex,
				"pageSize":   searchRequest.PageSize,
				"modes":      strings.Join(searchRequest.Modes, ","),
			}).Info("CHRob search page fetched")

			totalShipments += len(searchResponse.Results)

			var pageOrders []loader.LoaderOrder
			var addedKeys int
			var zeroLoadNumber int
			if len(searchResponse.Results) > 0 {
				s := searchResponse.Results[0]
				log.WithFields(log.Fields{
					"sample_loadNumber": s.LoadNumber,
					"sample_modes":      strings.Join(s.Modes, ","),
					"sample_origin":     fmt.Sprintf("%s, %s %s", s.Origin.City, firstNonEmpty(s.Origin.State, s.Origin.StateCode), firstNonEmpty(s.Origin.Zip, s.Origin.PostalCode)),
					"sample_dest":       fmt.Sprintf("%s, %s %s", s.Destination.City, firstNonEmpty(s.Destination.State, s.Destination.StateCode), firstNonEmpty(s.Destination.Zip, s.Destination.PostalCode)),
					"sample_miles":      s.Distance.Miles,
					"sample_weight_lb":  s.Weight.Pounds,
					"sample_equipment":  firstNonEmpty(s.SpecializedEquipment.Description, s.SpecializedEquipment.Code),
				}).Debug("CHRob response sample (first result)")
			}
			for _, shipment := range searchResponse.Results {
				if shipment.LoadNumber == 0 {
					zeroLoadNumber++
				}
				orderPayload := mapShipmentToLoaderOrder(shipment)
				key := chrobDedupKey(shipment, orderPayload)
				if _, exists := seenKeys[key]; exists {
					continue
				}
				seenKeys[key] = struct{}{}
				addedKeys++
				pageOrders = append(pageOrders, orderPayload)

				if name := strings.TrimSpace(shipment.Contact.CompanyName); name != "" {
					companyNameCounts[name]++
				}
				if name := strings.TrimSpace(shipment.Origin.Name); name != "" {
					originNameCounts[name]++
				}
				if name := strings.TrimSpace(shipment.Destination.Name); name != "" {
					destNameCounts[name]++
				}
			}

			log.WithFields(log.Fields{
				"lat":              lat,
				"lng":              lng,
				"pageIndex":        pageIndex,
				"results":          len(searchResponse.Results),
				"enqueued_unique":  len(pageOrders),
				"dedup_skipped":    len(searchResponse.Results) - len(pageOrders),
				"loadNumber_zero":  zeroLoadNumber,
				"loadNumber_non0":  len(searchResponse.Results) - zeroLoadNumber,
				"totalCount_field": searchResponse.TotalCount,
			}).Info("CHRob page summary")

			// If the API ignores pageIndex or returns a repeated page, we can end up in an infinite loop.
			// Break when this page contains no new unique loads.
			if pageIndex > 0 && len(pageOrders) == 0 {
				log.WithFields(log.Fields{
					"lat":        lat,
					"lng":        lng,
					"pageIndex":  pageIndex,
					"pageSize":   searchRequest.PageSize,
					"totalCount": searchResponse.TotalCount,
				}).Warn("CHRob paging yielded 0 new unique loads; stopping pagination for location")
				break
			}

			if shipmentKeyWarning := addedKeys == 0 && len(searchResponse.Results) > 0; shipmentKeyWarning {
				log.WithFields(log.Fields{
					"lat":       lat,
					"lng":       lng,
					"pageIndex": pageIndex,
					"results":   len(searchResponse.Results),
				}).Warn("CHRob page produced results but none were enqueued (all duplicates)")
			}

			// Prototype-test UI mode: do not POST to Loader API.
			// Instead, load the in-memory feed so the UI can page through results.
			if feed != nil {
				for _, o := range pageOrders {
					feed.Add(o)
					totalEnqueued++
				}
				log.WithFields(log.Fields{
					"pageIndex":       pageIndex,
					"enqueued":        len(pageOrders),
					"enqueued_total":  totalEnqueued,
					"location_lat":    lat,
					"location_lng":    lng,
					"location_source": "CHROBINSON",
				}).Info("CHRob enqueued into UI feed")
			} else {
				log.WithFields(log.Fields{
					"pageIndex":    pageIndex,
					"location_lat": lat,
					"location_lng": lng,
				}).Warn("UI feed store is nil; cannot enqueue CHRob orders")
			}

			// Stop when we've exhausted the result set.
			if len(searchResponse.Results) < searchRequest.PageSize {
				break
			}
			if searchResponse.TotalCount > 0 && (pageIndex+1)*searchRequest.PageSize >= searchResponse.TotalCount {
				break
			}

			pageIndex++
		}
	}

	log.WithFields(log.Fields{
		"processed_locations":    processedLocations,
		"skipped_locations":      skippedLocations,
		"search_errors":          searchErrors,
		"shipments_total":        totalShipments,
		"enqueued_total":         totalEnqueued,
		"posted_total":           totalPosted,
		"contact_company_unique": len(companyNameCounts),
		"origin_name_unique":     len(originNameCounts),
		"dest_name_unique":       len(destNameCounts),
		"contact_company_top":    topCounts(companyNameCounts, 5),
		"origin_name_top":        topCounts(originNameCounts, 5),
		"dest_name_top":          topCounts(destNameCounts, 5),
	}).Info("✅ ChrobSearchProcess completed")
	return nil
}

func StartChrobRunner(client *chrobinson.APIClient, feed *uifeed.Store) {
	go func() {
		log.WithFields(log.Fields{
			"runner": "CHROBINSON",
		}).Info("Runner started")

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		runOnce := func() {
			start := time.Now()
			log.WithFields(log.Fields{
				"runner": "CHROBINSON",
			}).Info("Runner cycle start")

			if err := ChrobSearchProcess(client, feed); err != nil {
				log.WithError(err).WithFields(log.Fields{
					"runner":       "CHROBINSON",
					"duration_ms":  time.Since(start).Milliseconds(),
					"completed_at": time.Now().Format(time.RFC3339),
				}).Error("Runner cycle failed")
				return
			}

			log.WithFields(log.Fields{
				"runner":      "CHROBINSON",
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

	pickupTime := timeOfDay(pickupDate)
	deliveryTime := timeOfDay(deliveryDate)

	pickupState := firstNonEmpty(shipment.Origin.State, shipment.Origin.StateCode)
	deliveryState := firstNonEmpty(shipment.Destination.State, shipment.Destination.StateCode)

	pickupZip := firstNonEmpty(shipment.Origin.Zip, shipment.Origin.PostalCode)
	deliveryZip := firstNonEmpty(shipment.Destination.Zip, shipment.Destination.PostalCode)

	pickupCountry := defaultCountry(firstNonEmpty(shipment.Origin.Country, countryFromCode(shipment.Origin.CountryCode)))
	deliveryCountry := defaultCountry(firstNonEmpty(shipment.Destination.Country, countryFromCode(shipment.Destination.CountryCode)))

	pickupLocation := formatLocation(shipment.Origin.City, pickupState, pickupCountry)
	deliveryLocation := formatLocation(shipment.Destination.City, deliveryState, deliveryCountry)

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
		PickupTime:          pickupTime,
		DeliveryTime:        deliveryTime,
		SuggestedTruckSize:  suggestedTruckSize,
		TruckTypeId:         truckTypeID,
		OriginalTruckSize:   originalTruckSize,
		PickupZip:           pickupZip,
		DeliveryZip:         deliveryZip,
		PickupCity:          shipment.Origin.City,
		PickupState:         pickupState,
		PickupCountry:       pickupCountry,
		PickupCountryCode:   countryCode(pickupCountry),
		PickupCountryName:   countryName(pickupCountry),
		DeliveryCity:        shipment.Destination.City,
		DeliveryState:       deliveryState,
		DeliveryCountry:     deliveryCountry,
		DeliveryCountryCode: countryCode(deliveryCountry),
		DeliveryCountryName: countryName(deliveryCountry),
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
		EmptyDateTime:       deliveryDate,
		EmptyLocation: loader.EmptyLocation{
			City:    shipment.Destination.City,
			State:   deliveryState,
			Country: deliveryCountry,
			Zip:     deliveryZip,
		},
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

func countryName(country string) string {
	if strings.EqualFold(country, "USA") || strings.EqualFold(country, "United States") {
		return "United States"
	}
	return country
}

func countryFromCode(code string) string {
	if strings.EqualFold(strings.TrimSpace(code), "US") {
		return "USA"
	}
	return ""
}

func timeOfDay(rfc3339OrDate string) string {
	if strings.TrimSpace(rfc3339OrDate) == "" {
		return ""
	}
	if t, ok := parseDateTime(rfc3339OrDate); ok {
		return t.Format("15:04")
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

type kv struct {
	Key   string
	Count int
}

func topCounts(m map[string]int, n int) []kv {
	if n <= 0 || len(m) == 0 {
		return []kv{}
	}
	items := make([]kv, 0, len(m))
	for k, c := range m {
		items = append(items, kv{Key: k, Count: c})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].Key < items[j].Key
		}
		return items[i].Count > items[j].Count
	})
	if len(items) > n {
		items = items[:n]
	}
	return items
}
