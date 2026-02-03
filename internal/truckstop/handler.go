package truckstop

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"truckapi/internal/chrobinson"
	"truckapi/internal/uifeed"
	"truckapi/pkg/config"

	"github.com/gofiber/fiber/v2"

	log "github.com/sirupsen/logrus"
)

// func TruckstopSearchHandler(client *LoadSearchClient) fiber.Handler {
// 	return func(c *fiber.Ctx) error {
// 		combinedInfos, err := db.GetActiveTrucksAndLocations()
// 		if err != nil {
// 			log.WithError(err).Error("Failed to get trucks and locations")
// 			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal error"})
// 		}

// 		if len(combinedInfos) == 0 {
// 			return c.Status(fiber.StatusOK).JSON([]Load{})
// 		}

// 		var allLoads []Load

// 		for _, info := range combinedInfos {
// 			fromDate := info.LocationData.From[:10]
// 			fromParsed, _ := time.Parse("2006-01-02", fromDate)
// 			toDate := fromParsed.AddDate(0, 0, 10).Format("2006-01-02")

// 			request := BuildLoadSearchRequest(info, fromDate, toDate, client)

// 			loads, err := client.SearchLoads(request)
// 			if err != nil {
// 				log.WithError(err).Errorf("Truckstop load search failed for truck ID %d", info.TruckData.Id)
// 				continue
// 			}
// 			allLoads = append(allLoads, loads...)
// 		}

// 		return c.JSON(allLoads)
// 	}
// }

func BuildLoadSearchRequest(info chrobinson.CombinedShipmentInfo, fromDate, toDate string, client *LoadSearchClient) LoadSearchRequest {
	originCity := ExtractCity(info.LocationData.Address)
	originState := ExtractStateCode(info.LocationData.Address)
	pickupDates := []string{fromDate, toDate}

	var truckType string
	switch info.TruckData.TruckTypeId {
	case 1:
		truckType = "SMALL STRAIGHT"
	case 2:
		truckType = "LARGE STRAIGHT"
	case 3:
		truckType = "SPRINTER"
	default:
		truckType = "unknown"
	}

	return LoadSearchRequest{
		IntegrationId: client.IntegrationID,
		UserName:      client.Username,
		Password:      client.Password,
		Criteria: LoadSearchCriteria{
			DestinationCity:    "",
			DestinationState:   "",
			DestinationCountry: "USA",
			DestinationRange:   0,

			OriginCity:    originCity,
			OriginState:   originState,
			OriginCountry: "USA",
			OriginRange:   250,

			EquipmentType: GetMappedEquipmentCodes(truckType),
			LoadType:      All,
			HoursOld:      48,

			PageNumber:     1,
			PageSize:       100,
			PickupDates:    &pickupDates,
			SortBy:         PickUpDate,
			SortDescending: false,
		},
	}
}

func TruckstopSearchHandler(client *LoadSearchClient, feed *uifeed.Store) fiber.Handler {
	return func(c *fiber.Ctx) error {
		err := TruckstopSearchProcess(client, feed)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		return c.JSON(fiber.Map{
			"message": "Truckstop loads processed and sent to UI feed",
		})
	}
}

func (c *LoadSearchClient) SearchLoads(req LoadSearchRequest) ([]Load, error) {
	log.Info("🚀 Preparing SOAP request")

	// 1) marshal <v12:searchRequest>
	xmlData, err := xml.Marshal(req)
	if err != nil {
		log.WithError(err).Error("❌ Failed to marshal request")
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// 2) wrap in full SOAP envelope
	soapEnvelope := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/">
  <soapenv:Header/>
  <soapenv:Body>
    <v12:GetLoadSearchResults
        xmlns:v12="http://webservices.truckstop.com/v12"
        xmlns:web="http://schemas.datacontract.org/2004/07/WebServices"
        xmlns:web1="http://schemas.datacontract.org/2004/07/WebServices.Searching"
        xmlns:truc="http://schemas.datacontract.org/2004/07/Truckstop2.Objects"
        xmlns:arr="http://schemas.microsoft.com/2003/10/Serialization/Arrays">
      %s
    </v12:GetLoadSearchResults>
  </soapenv:Body>
</soapenv:Envelope>`, string(xmlData))

	log.Info("🚀 Sending SOAP request to Truckstop API…")
	log.Debugf("🔍 SOAP Request:\n%s", soapEnvelope)

	// 3) HTTP POST to Truckstop
	httpReq, err := http.NewRequest(
		http.MethodPost,
		c.LoadSearchURL,
		bytes.NewBufferString(soapEnvelope),
	)
	if err != nil {
		log.WithError(err).Error("❌ Failed to create HTTP request")
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "text/xml")
	httpReq.Header.Set("SOAPAction", "http://webservices.truckstop.com/v12/ILoadSearch/GetLoadSearchResults")

	resp, err := c.Client.Do(httpReq)
	if err != nil {
		log.WithError(err).Error("❌ HTTP request failed")
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	log.Infof("🔔 HTTP response received – status_code=%d", resp.StatusCode)

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.WithError(err).Error("❌ Failed to read response body")
		return nil, fmt.Errorf("read body: %w", err)
	}
	// log.Debugf("📨 RAW SOAP RESPONSE:\n%s", string(bodyBytes))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API status %d: %s", resp.StatusCode, bodyBytes)
	}

	// 4) Unmarshal into Envelope
	var envelope Envelope
	if err := xml.Unmarshal(bodyBytes, &envelope); err != nil {
		log.WithError(err).Error("❌ Unmarshal failure")
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	result := envelope.Body.Response.Result

	// 5) Handle Truckstop API errors
	if len(result.Errors) > 0 {
		log.WithField("errors", result.Errors).Error("❌ API reported errors")
		return nil, fmt.Errorf("API error: %s", result.Errors[0].ErrorMessage)
	}

	log.Infof("✅ Load search completed successfully – count=%d", len(result.SearchResults))
	return result.SearchResults, nil
}

// func HandleTruckstopWebSocketConnection(client *LoadSearchClient, conn *websocket.Conn) {
// 	type wsMsg struct {
// 		messageType int
// 		data        []byte
// 	}

// 	writeCh := make(chan wsMsg, 200) // buffered
// 	ctx, cancel := context.WithCancel(context.Background())
// 	defer cancel() // ensures goroutines stop on exit

// 	// single writer goroutine
// 	go func() {
// 		for {
// 			select {
// 			case <-ctx.Done():
// 				return
// 			case m, ok := <-writeCh:
// 				if !ok { // channel closed elsewhere (shouldn’t happen)
// 					return
// 				}
// 				if err := conn.WriteMessage(m.messageType, m.data); err != nil {
// 					log.WithError(err).Error("❌ WebSocket write failed — closing connection")
// 					conn.Close()
// 					cancel() // signal all helpers to stop
// 					return
// 				}
// 			}
// 		}
// 	}()

// 	conn.SetReadLimit(512)
// 	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
// 	conn.SetPongHandler(func(string) error {
// 		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
// 		return nil
// 	})

// 	// 2) Ping ticker also uses the same mutex
// 	go func() {
// 		ticker := time.NewTicker(30 * time.Second)
// 		defer ticker.Stop()

// 		for range ticker.C {
// 			select {
// 			case writeCh <- wsMsg{messageType: websocket.PingMessage, data: nil}:
// 			case <-ctx.Done():
// 				return
// 			}
// 		}
// 	}()

// 	for {
// 		combinedInfos, err := db.GetActiveTrucksAndLocationsTruckStop()
// 		if err != nil {
// 			log.WithError(err).Error("❌ Failed to get active trucks and locations")
// 			cancel()
// 			return
// 		}
// 		if len(combinedInfos) == 0 {
// 			log.Info("ℹ️ No active trucks or locations found for today.")
// 			cancel()
// 			return
// 		}

// 		totalLoadsFound := 0

// 		for _, info := range combinedInfos {
// 			log.Infof("🔍 Searching loads for truck ID %d (%s)", info.TruckData.Id, info.LocationData.Address)

// 			from := time.Now().Format("2006-01-02")
// 			to := time.Now().Add(48 * time.Hour).Format("2006-01-02")
// 			req := BuildLoadSearchRequest(info, from, to, client)

// 			loads, err := client.SearchLoads(req)
// 			if err != nil {
// 				log.WithError(err).
// 					Errorf("❌ Load search failed for truck ID %d", info.TruckData.Id)
// 				continue
// 			}
// 			totalLoadsFound += len(loads)

// 			for _, load := range loads {

// 				// Force booking contact phone empty for test
// 				// load.PointOfContactPhone = ""

// 				payload := map[string]interface{}{
// 					"load":                      load,
// 					"truckData":                 info.TruckData,
// 					"locationData":              info.LocationData,
// 					"additionalData":            info.AdditionalData,
// 					"bookingContactPhoneNumber": load.PointOfContactPhone,
// 				}
// 				data, err := json.Marshal(payload)
// 				if err != nil {
// 					log.WithError(err).Error("❌ Failed to marshal load to JSON")
// 					continue
// 				}

// 				select {
// 				case writeCh <- wsMsg{messageType: websocket.TextMessage, data: data}:
// 				case <-ctx.Done():
// 					return
// 				}

// 				// Send to SQS after writing to the websocket
// 				if err := SendToTruckapiSQS(payload); err != nil {
// 					log.WithError(err).Error("❌ Failed to send message to SQS from TruckAPI")
// 				}
// 			}
// 		}

// 		log.Infof("✅ Truckstop search done – %d trucks, %d loads", len(combinedInfos), totalLoadsFound)

// 		endMsg := map[string]string{"type": "end-of-batch"}
// 		data, _ := json.Marshal(endMsg)
// 		select {
// 		case writeCh <- wsMsg{messageType: websocket.TextMessage, data: data}:
// 		case <-ctx.Done():
// 			return
// 		}

// 		log.Info("😴 Sleeping for 3 minutes...")
// 		time.Sleep(3 * time.Minute)
// 	}
// }

func ExtractCity(address string) string {
	parts := strings.Split(address, ",")
	if len(parts) >= 3 {
		return strings.TrimSpace(parts[len(parts)-3])
	}
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0])
	}
	return address
}

func ExtractZip(address string) string {
	parts := strings.Split(address, ",")
	if len(parts) < 2 {
		return ""
	}
	secondLast := strings.TrimSpace(parts[len(parts)-2])
	fields := strings.Fields(secondLast)
	if len(fields) > 1 {
		return fields[1] // e.g. "90015"
	}
	return ""
}

func ExtractStateCode(address string) string {
	parts := strings.Split(address, ",")
	if len(parts) < 2 {
		return ""
	}

	// Take the second-to-last part (usually contains: "CA 90015" or "NC 28732")
	secondLast := strings.TrimSpace(parts[len(parts)-2])
	fields := strings.Fields(secondLast)

	if len(fields) > 0 {
		return fields[0] // State code like "CA"
	}
	return ""
}

func GetMappedEquipmentCodes(truckType string) string {
	switch strings.ToUpper(strings.TrimSpace(truckType)) {
	case "SMALL STRAIGHT":
		return "V, VA, SV" // simple van
	case "SPRINTER":
		return "SV,CV,VCAR" // sprinter family
	case "LARGE STRAIGHT":
		return "VLG,VA,HS" // liftgate, air-ride, hot shot
	default:
		return "FVR,VRF,CV" // fallback for unknown types
	}
}

// // SendToTruckapiSQS sends the given payload to the TruckAPI SQS queue.
// func SendToTruckapiSQS(payload interface{}) error {
// 	sess := session.Must(session.NewSession(&aws.Config{
// 		Region: aws.String("us-east-1"),
// 	}))
// 	sqsClient := sqs.New(sess)

// 	jsonBytes, err := json.Marshal(payload)
// 	if err != nil {
// 		return err
// 	}

// 	msgID := uuid.New().String()

// 	_, err = sqsClient.SendMessage(&sqs.SendMessageInput{
// 		QueueUrl:               aws.String("https://sqs.us-east-1.amazonaws.com/333767869901/pasrer.fifo"),
// 		MessageBody:            aws.String(string(jsonBytes)),
// 		MessageGroupId:         aws.String("truckapi-parser"),
// 		MessageDeduplicationId: aws.String(msgID),
// 	})
// 	return err
// }

// func TruckstopSearchHandler(client *LoadSearchClient) fiber.Handler {
// 	return func(c *fiber.Ctx) error {
// 		locations, err := db.FetchLoaderLocations("TRUCKSTOP")
// 		if err != nil {
// 			log.WithError(err).Error("Failed to fetch locations from Loader API")
// 			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal error"})
// 		}

// 		for _, loc := range locations {
// 			originCity := ExtractCity(loc.Address)
// 			originState := ExtractStateCode(loc.Address)

// 			fromDate := time.Now().Format("2006-01-02")
// 			toDate := time.Now().AddDate(0, 0, 10).Format("2006-01-02")
// 			pickupDates := []string{fromDate, toDate}

// 			request := LoadSearchRequest{
// 				IntegrationId: client.IntegrationID,
// 				UserName:      client.Username,
// 				Password:      client.Password,
// 				Criteria: LoadSearchCriteria{
// 					OriginCity:         originCity,
// 					OriginState:        originState,
// 					OriginCountry:      "USA",
// 					OriginRange:        250,
// 					DestinationCity:    "",
// 					DestinationState:   "",
// 					DestinationCountry: "USA",
// 					DestinationRange:   0,
// 					EquipmentType:      "", // we'll filter results later
// 					LoadType:           All,
// 					HoursOld:           48,
// 					PageNumber:         1,
// 					PageSize:           100,
// 					PickupDates:        &pickupDates,
// 					SortBy:             PickUpDate,
// 					SortDescending:     false,
// 				},
// 			}

// 			details, err := client.GetMultipleLoadDetails(request)
// 			if err != nil {
// 				log.WithError(err).Errorf("Truckstop multi-load detail search failed for address %s", loc.Address)
// 				continue
// 			}

// 			for _, load := range details {
// 				// Map equipment type
// 				mapping, ok := TruckstopEquipmentMapping[load.Equipment]
// 				if !ok {
// 					log.Warnf("Skipping load %s because equipment type %s is not mapped", load.ID, load.Equipment)
// 					continue
// 				}

// 				// Parse pickup date
// 				var pickupTime time.Time
// 				if load.PickUpDate != "" {
// 					// Try to parse as RFC3339 first
// 					var err error
// 					pickupTime, err = time.Parse(time.RFC3339, load.PickUpDate)
// 					if err != nil {
// 						// fallback: try legacy Truckstop date format MM/DD/YY
// 						pickupTime, err = time.Parse("01/02/06", load.PickUpDate)
// 						if err != nil {
// 							log.WithError(err).Warnf("Unable to parse pickup date for load %s. Using current time.", load.ID)
// 							pickupTime = time.Now()
// 						}
// 					}
// 				} else {
// 					pickupTime = time.Now()
// 				}

// 				pickupDateISO := pickupTime.Format(time.RFC3339)
// 				deliveryDateISO := pickupTime.Add(24 * time.Hour).Format(time.RFC3339)

// 				// Defensive nil-checks for numeric fields
// 				miles := float64(load.Mileage)
// 				payment := float64(load.PaymentAmount)
// 				length := float64(load.Length)
// 				width := float64(load.Width)
// 				weight := float64(load.Weight)

// 				height := 0.0
// 				if width == 0 {
// 					width = 102.0
// 				}

// 				carrierPayRate := 0.0
// 				if miles > 0 {
// 					carrierPayRate = payment / miles
// 				}

// 				pickupLocation := fmt.Sprintf("%s, %s, %s, USA", load.OriginZip, load.OriginCity, load.OriginState)
// 				deliveryLocation := fmt.Sprintf("%s, %s, %s, USA", load.DestinationZip, load.DestinationCity, load.DestinationState)

// 				orderPayload := LoaderOrder{
// 					Source:             "TRUCKSTOP",
// 					OrderNumber:        fmt.Sprintf("TS-%s", load.ID),
// 					PickupLocation:     pickupLocation,
// 					DeliveryLocation:   deliveryLocation,
// 					PickupDate:         pickupDateISO,
// 					DeliveryDate:       deliveryDateISO,
// 					SuggestedTruckSize: mapping.SuggestedTruckSize,
// 					TruckTypeId:        mapping.TruckTypeId,
// 					OriginalTruckSize:  load.Equipment,
// 					PickupZip:          load.OriginZip,
// 					DeliveryZip:        load.DestinationZip,
// 					PickupCity:         load.OriginCity,
// 					PickupState:        load.OriginState,
// 					PickupCountry:      "USA",
// 					DeliveryCity:       load.DestinationCity,
// 					DeliveryState:      load.DestinationState,
// 					DeliveryCountry:    "USA",
// 					EstimatedMiles:     miles,
// 					OrderTypeId:        5,
// 					Length:             length,
// 					Width:              width,
// 					Height:             height,
// 					Weight:             weight,
// 					Pieces:             20,
// 					Stackable:          true,
// 					Hazardous:          false,
// 					CarrierPay:         payment,
// 					CarrierPayRate:     carrierPayRate,
// 					ReplyTo:            "logistics@fullcircle.com",
// 					Subject:            fmt.Sprintf("TS Order TS-%s", load.ID),
// 					BodyHTML:           "<html>...</html>",
// 					BodyPlain:          "Plain text email body content...",
// 					MessageId:          "<message-id@mailgun.org>",
// 					ParserLogId:        67,
// 					CreatedAt:          time.Now().Format(time.RFC3339),
// 					UpdatedAt:          time.Now().Format(time.RFC3339),
// 				}

// 				payloadBytes, err := json.Marshal(orderPayload)
// 				if err != nil {
// 					log.WithError(err).Error("Failed to marshal order payload")
// 					continue
// 				}

// 				req, err := http.NewRequest(
// 					"POST",
// 					"http://54.89.251.35/api/v1/loader/orders",
// 					bytes.NewBuffer(payloadBytes),
// 				)
// 				if err != nil {
// 					log.WithError(err).Error("Failed to create POST request to Loader API")
// 					continue
// 				}
// 				req.Header.Set("Content-Type", "application/json")
// 				req.Header.Set("X-API-KEY", "loaderBMwuIUZKtyH8fetLykDch07dxfciUZZ8lrGqOfmVaAjnXAhcwIRIdBCyhg")

// 				resp, err := http.DefaultClient.Do(req)
// 				if err != nil {
// 					log.WithError(err).Error("Failed to send POST request to Loader API")
// 					continue
// 				}
// 				defer resp.Body.Close()

// 				if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
// 					body, _ := io.ReadAll(resp.Body)
// 					log.Errorf("Loader API responded with status %d: %s", resp.StatusCode, string(body))
// 				} else {
// 					log.Infof("✅ Successfully posted order %s to Loader API", orderPayload.OrderNumber)
// 				}
// 			}
// 		}

// 		return c.JSON(fiber.Map{"message": "Truckstop loads processed and posted to Loader API"})
// 	}
// }

func (c *LoadSearchClient) GetMultipleLoadDetails(req LoadSearchRequest) ([]LoadDetail, string, error) {
	log.Info("🚀 Preparing SOAP request for multiple load details")

	// marshal searchRequest XML
	xmlData, err := xml.Marshal(req)
	if err != nil {
		return nil, "", fmt.Errorf("marshal request: %w", err)
	}

	// wrap in SOAP envelope
	soapEnvelope := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/">
  <soapenv:Header/>
  <soapenv:Body>
    <v12:GetMultipleLoadDetailResults
        xmlns:v12="http://webservices.truckstop.com/v12"
        xmlns:web="http://schemas.datacontract.org/2004/07/WebServices"
        xmlns:web1="http://schemas.datacontract.org/2004/07/WebServices.Searching"
        xmlns:truc="http://schemas.datacontract.org/2004/07/Truckstop2.Objects"
        xmlns:arr="http://schemas.microsoft.com/2003/10/Serialization/Arrays">
      %s
    </v12:GetMultipleLoadDetailResults>
  </soapenv:Body>
</soapenv:Envelope>`, string(xmlData))

	// prepare HTTP request
	httpReq, err := http.NewRequest(
		http.MethodPost,
		c.LoadSearchURL,
		bytes.NewBufferString(soapEnvelope),
	)
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "text/xml")
	httpReq.Header.Set("SOAPAction", "http://webservices.truckstop.com/v12/ILoadSearch/GetMultipleLoadDetailResults")

	resp, err := c.Client.Do(httpReq)
	if err != nil {
		return nil, "", fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read body: %w", err)
	}

	// Avoid dumping huge SOAP/HTML payloads to stdout by default.
	// If you need to see raw bodies, set `TRUCKSTOP_DEBUG_BODY=true`.
	if v := strings.ToLower(strings.TrimSpace(config.GetEnv("TRUCKSTOP_DEBUG_BODY", "false"))); v == "1" || v == "true" || v == "yes" {
		const max = 16 * 1024
		s := string(bodyBytes)
		if len(s) > max {
			s = s[:max] + "\n…(truncated)…\n"
		}
		log.WithFields(log.Fields{
			"status":     resp.StatusCode,
			"body_bytes": len(bodyBytes),
		}).Infof("🚨 RAW SOAP response:\n%s", s)
	}

	if resp.StatusCode != http.StatusOK {
		const max = 16 * 1024
		s := string(bodyBytes)
		if len(s) > max {
			s = s[:max] + "\n…(truncated)…\n"
		}
		return nil, string(bodyBytes), fmt.Errorf("API error %d: %s", resp.StatusCode, s)
	}

	// unmarshal into MultipleLoadDetailEnvelope
	var envelope MultipleLoadDetailEnvelope
	if err := xml.Unmarshal(bodyBytes, &envelope); err != nil {
		const max = 16 * 1024
		s := string(bodyBytes)
		if len(s) > max {
			s = s[:max] + "\n…(truncated)…\n"
		}
		return nil, string(bodyBytes), fmt.Errorf("unmarshal: %w; body: %s", err, s)
	}

	loads := envelope.Body.Response.Result.DetailResults.Loads
	log.Infof("✅ Fetched %d load details from Truckstop", len(loads))
	return loads, string(bodyBytes), nil
}
