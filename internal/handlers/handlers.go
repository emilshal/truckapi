package handlers

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"truckapi/db"
	"truckapi/internal/chrobinson"
	"truckapi/internal/loader"
	"truckapi/pkg/config"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
)

func EventCallbackHandler(c *fiber.Ctx) error {
	var event chrobinson.Event // Define your event type based on the webhook payload structure
	if err := c.BodyParser(&event); err != nil {
		log.WithError(err).Error("Failed to parse event payload")
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid event payload"})
	}

	eventTypeDescription := event.PlatformEventType()
	if eventTypeDescription == "" {
		log.Errorf("Unknown event type: %s", event.Event.EventType)
		return fiber.NewError(fiber.StatusNotFound, "The event type provided is not recognized")
	}
	//Log event type and time
	log.Infof("Received event: %s - %s", eventTypeDescription, event.Event.EventType)

	// Log every field inside the event
	log.Infof("Event details: %+v", event)

	return c.SendStatus(fiber.StatusOK)
}

func HandleDriverData(c *fiber.Ctx) error {
	var data chrobinson.DriverData
	if err := c.BodyParser(&data); err != nil {
		log.WithError(err).Error("Failed to parse driver data")
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	//For now we log this data, but we can do anything with it here
	log.Infof("Received driver data: %+v", data)

	//Return the data as a JSON response
	return c.JSON(fiber.Map{"message": "Driver data received", "data": data})

}

// SearchAvailableShipmentsHandler creates a fiber.Handler that handles requests to search for available shipments.
func SearchAvailableShipmentsHandler(apiClient *chrobinson.APIClient) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Define a variable to hold the incoming search request.
		var searchRequest chrobinson.AvailableShipmentSearchRequest

		// Parse the JSON request body into the searchRequest struct.
		if err := c.BodyParser(&searchRequest); err != nil {
			// If parsing fails, log the error and return a 400 Bad Request status.
			log.WithError(err).Error("Failed to parse search request")
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}

		// After parsing the request body
		log.Infof("Parsed search request: %+v", searchRequest)
		if strings.TrimSpace(searchRequest.CarrierCode) == "" {
			searchRequest.CarrierCode = config.GetEnv(config.CHRobCarrierCode, "")
		}

		// Define a variable to hold the search response.
		var searchResponse *chrobinson.AvailableShipmentSearchResponse

		// Use HandleAPICall to make the API call and handle token refresh if needed.
		err := chrobinson.HandleAPICall(apiClient, func() error {
			// Call the SearchAvailableShipments method of the APIClient to search for available shipments.
			response, err := apiClient.SearchAvailableShipments(searchRequest)
			if err != nil {
				return err
			}
			// Assign the response to the searchResponse variable.
			searchResponse = response
			for _, shipment := range searchResponse.Results {
				if shipment.LoadNumber > 0 && len(shipment.AvailableLoadCosts) > 0 {
					chrobinson.CacheAvailableLoadCosts(shipment.LoadNumber, shipment.AvailableLoadCosts)
				}
				if shipment.LoadNumber > 0 {
					chrobinson.CachePickupDefaults(shipment.LoadNumber, shipment.Origin, firstNonEmptyBooking(shipment.CalculatedPickUpByDateTime, shipment.PickUpByDate, shipment.ReadyBy))
				}
			}
			// Before sending the response
			log.Infof("Sending search response: %+v", searchResponse)
			return nil
		})

		// If there's an error, log it and return an appropriate HTTP status code and error message.
		if err != nil {
			log.WithError(err).Error("Failed to search for available shipments")
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}

		// Commented out logic for sending the search response to an external endpoint
		/*
			// Prepare the request payload
			payload, err := json.Marshal(searchResponse)
			if err != nil {
				log.WithError(err).Error("Failed to marshal search response for external API")
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
			}

			// Send the response to the external endpoint
			req, err := http.NewRequest("POST", "https://platform.hfield.net/api/loadboards/receive/chrob", bytes.NewBuffer(payload))
			if err != nil {
				log.WithError(err).Error("Failed to create request for external API")
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
			}
			req.Header.Set("Content-Type", "application/json")

			// Execute the request
			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				log.WithError(err).Error("Failed to send request to external API")
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
			}
			defer resp.Body.Close()

			// Check the response status
			if resp.StatusCode != http.StatusOK {
				body, _ := ioutil.ReadAll(resp.Body)
				log.Errorf("External API responded with status %d: %s", resp.StatusCode, string(body))
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to send data to external API"})
			}
		*/

		// Return a 200 OK status with the search response.
		return c.Status(fiber.StatusOK).JSON(searchResponse)
	}
}

// CombinedShipmentInfoHandler handles requests for combined shipment information.
func CombinedShipmentInfoHandler(apiClient *chrobinson.APIClient) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// We no longer paginate here — we fetch all active trucks updated today
		combinedInfos, err := db.GetActiveTrucksAndLocations()
		if err != nil {
			log.WithError(err).Error("Failed to get active trucks and locations")
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}

		if len(combinedInfos) == 0 {
			log.Info("No active trucks or locations found for today.")
			return c.Status(fiber.StatusOK).JSON([]chrobinson.CombinedShipmentInfo{})
		}

		var allShipments []chrobinson.CombinedShipmentInfo

		for _, combinedInfo := range combinedInfos {
			shipments, err := db.SearchAvailableShipmentsForTruck(apiClient, combinedInfo)
			if err != nil {
				log.WithError(err).Errorf("Failed to search for shipments for truck ID %d", combinedInfo.TruckData.Id)
				continue
			}
			allShipments = append(allShipments, shipments...)
		}

		return c.Status(fiber.StatusOK).JSON(allShipments)
	}
}

// BookLoadHandler creates a fiber.Handler that handles requests to book a load.
func BookLoadHandler(apiClient *chrobinson.APIClient) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Parse the JSON body into the LoadBookingRequest struct
		var bookingRequest chrobinson.LoadBookingRequest
		if err := c.BodyParser(&bookingRequest); err != nil {
			log.WithError(err).Error("Failed to parse request body")
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid request data",
			})
		}

		// The Loader colleague sends snake_case aliases (t_number, load_number,
		// order_bid_id) that the native LoadBookingRequest struct doesn't
		// recognize. Parse them from the raw body and reconcile:
		//   - carrier: t_number / tNumber / carrierCode (must agree if >1 given)
		//   - loadNumber: load_number / loadNumber (accepts string or int)
		//   - order_bid_id: captured so the booking record can carry it for the
		//     eventual forward back to Loader.
		var bookAliases struct {
			TNumber         string          `json:"t_number"`
			TNumberCamel    string          `json:"tNumber"`
			CarrierCode     string          `json:"carrierCode"`
			LoadNumberSnake json.RawMessage `json:"load_number"`
			LoadNumberCamel json.RawMessage `json:"loadNumber"`
			OrderBidID      json.Number     `json:"order_bid_id"`
			OrderBidIDCamel json.Number     `json:"orderBidId"`
		}
		_ = json.Unmarshal(c.Body(), &bookAliases)

		resolvedCarrier, aliasErr := resolveCarrierAliases(bookAliases.TNumber, bookAliases.TNumberCamel, bookAliases.CarrierCode)
		if aliasErr != nil {
			if fe, ok := aliasErr.(*fiber.Error); ok {
				return c.Status(fe.Code).JSON(fiber.Map{"error": fe.Message})
			}
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid carrier identifier"})
		}
		if resolvedCarrier != "" {
			bookingRequest.CarrierCode = resolvedCarrier
		}

		// Reconcile the load number from the aliases when the native camelCase
		// field didn't populate it. The Loader stores CHRob orders with a
		// "CHROB-" prefix (that's how our runner posts them), so a book may
		// arrive as "CHROB-202237619" — strip the prefix and accept the digits.
		// Also tolerate plain string-encoded numbers ("202237619").
		if bookingRequest.LoadNumber == 0 {
			for _, raw := range []json.RawMessage{bookAliases.LoadNumberSnake, bookAliases.LoadNumberCamel} {
				if n, ok := parseLoadNumber(raw); ok {
					bookingRequest.LoadNumber = n
					break
				}
			}
		}

		// Capture the Loader's bid row id (snake or camel), accepting numeric or
		// string-encoded values.
		orderBidID := 0
		for _, raw := range []json.Number{bookAliases.OrderBidID, bookAliases.OrderBidIDCamel} {
			if raw == "" {
				continue
			}
			if n, convErr := strconv.Atoi(raw.String()); convErr == nil && n != 0 {
				if orderBidID != 0 && orderBidID != n {
					return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
						"error": "order_bid_id and orderBidId must match when both are provided",
					})
				}
				orderBidID = n
			}
		}

		if bookingRequest.CarrierCode == "" {
			bookingRequest.CarrierCode = config.GetEnv(config.CHRobCarrierCode, "")
		}
		if bookingRequest.LoadNumber == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "loadNumber is required",
			})
		}

		// CHRob requires emptyLocation, emptyDateTime, and rateConfirmation. The
		// Loader colleague sends only load/carrier/bid ids, so default the
		// logistics fields from the load's pickup origin (a truck arriving empty
		// at the pickup on the pickup date) when the caller omits them. Any field
		// the caller DID send is preserved.
		applyBookingDefaults(&bookingRequest)

		if err := populateBookingRequestFromOffer(&bookingRequest); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": err.Error(),
			})
		}
		if bookingRequest.CarrierCode == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "carrierCode is required",
			})
		}
		if len(bookingRequest.AvailableLoadCosts) == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "availableLoadCosts must include at least one item",
			})
		}

		rawRequest, err := json.Marshal(bookingRequest)
		if err != nil {
			log.WithError(err).Error("Failed to marshal booking request")
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to process booking request",
			})
		}

		// Use HandleAPICall to make the API call and handle token refresh if needed.
		err = chrobinson.HandleAPICall(apiClient, func() error {
			return apiClient.BookLoad(bookingRequest)
		})

		// Handle errors from the API call or token handling.
		if err != nil {
			log.WithError(err).Error("Failed to book load")
			// Determine the response status code based on the error type or content
			if strings.Contains(err.Error(), "status code 400") {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "Bad request to API",
				})
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to process booking",
			})
		}

		loadCostsJSON, marshalErr := json.Marshal(bookingRequest.AvailableLoadCosts)
		if marshalErr != nil {
			log.WithError(marshalErr).Error("Failed to marshal availableLoadCosts for in-memory booking tracking")
		} else {
			runtimeStore.addBooking(chrobinson.LoadBookingRecord{
				LoadNumber:            bookingRequest.LoadNumber,
				CarrierCode:           bookingRequest.CarrierCode,
				OrderBidID:            orderBidID,
				Status:                "accepted",
				EmptyDateTime:         bookingRequest.EmptyDateTime,
				RateConfirmationName:  bookingRequest.RateConfirmation.Name,
				RateConfirmationEmail: bookingRequest.RateConfirmation.Email,
				AvailableLoadCosts:    string(loadCostsJSON),
				RawRequest:            string(rawRequest),
			})
		}

		// If everything was successful, return an appropriate response
		response := fiber.Map{
			"message":                 "Load booked successfully",
			"loadNumber":              bookingRequest.LoadNumber,
			"carrierCode":             bookingRequest.CarrierCode,
			"t_number":                bookingRequest.CarrierCode,
			"status":                  "accepted",
			"persisted":               false,
			"awaitingShipmentDetails": true,
			"trackingMode":            "memory",
		}
		if orderBidID > 0 {
			response["order_bid_id"] = orderBidID
		}
		return c.Status(fiber.StatusAccepted).JSON(response)
	}
}

// OfferLoadHandler handles the offer load request and saves it to the database.
func OfferLoadHandler(c *fiber.Ctx) error {
	var offer chrobinson.OfferResponse
	if err := c.BodyParser(&offer); err != nil {
		log.WithError(err).Error("Failed to parse offer load request")
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid offer load request data",
		})
	}

	offer.Status = "pending"
	offer.RejectReasonsStr = chrobinson.ConvertRejectReasonsToString([]string{}) // Initialize with empty JSON array

	offer.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	offer.UpdatedAt = offer.CreatedAt
	offer = runtimeStore.upsertOffer(offer)

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Offer load saved successfully",
		"offerId": offer.ID,
	})
}

// FetchAllOffersHandler handles fetching all offer responses.
func FetchAllOffersHandler(c *fiber.Ctx) error {
	offers := runtimeStore.listOffers()

	// Convert RejectReasons field to slice of strings
	for i := range offers {
		reasons, err := chrobinson.ConvertRejectReasonsToSlice(offers[i].RejectReasonsStr)
		if err != nil {
			log.Println("Failed to parse reject reasons:", err)
			continue
		}
		offers[i].RejectReasons = reasons
	}

	return c.JSON(fiber.Map{
		"offers": offers,
	})
}

func FetchAllShipmentDetailsHandler(c *fiber.Ctx) error {
	records := runtimeStore.listShipmentDetails()

	return c.JSON(fiber.Map{
		"shipmentDetails": records,
	})
}

func FetchAllBookingsHandler(c *fiber.Ctx) error {
	records := runtimeStore.listBookings()

	return c.JSON(fiber.Map{
		"bookings": records,
	})
}

// OfferResponseHandler handles the callback for offer responses.

// OfferResponseHandler handles the callback for offer responses.
func OfferResponseHandler(c *fiber.Ctx) error {
	rawBody := append([]byte(nil), c.Body()...)
	var offerResponse chrobinson.OfferResponseCallback
	if err := json.Unmarshal(rawBody, &offerResponse); err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"body_len":     len(rawBody),
			"raw_body":     string(rawBody),
			"content_type": string(c.Request().Header.ContentType()),
		}).Error("Failed to parse offer response")
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid offer response data",
		})
	}

	logrus.WithFields(logrus.Fields{
		"loadNumber":     offerResponse.LoadNumber.Int(),
		"carrierCode":    offerResponse.CarrierCode,
		"offerRequestId": offerResponse.OfferRequestId,
		"offerId":        offerResponse.OfferId.String(),
		"offerResult":    offerResponse.OfferResult,
		"price":          offerResponse.Price.Int(),
		"currencyCode":   offerResponse.CurrencyCode,
		"rejectReasons":  offerResponse.RejectReasons,
	}).Info("Received offer response")
	if offerResponse.OfferRequestId == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "offerRequestId is required",
		})
	}

	// Determine the new status based on the offer result
	newStatus := offerResponse.OfferResult
	if newStatus == "Accepted" {
		newStatus = "booked"
	} else if newStatus == "Rejected" || newStatus == "NotConsidered" {
		newStatus = "declined"
	} else if newStatus == "Counter" {
		newStatus = "countered"
	}

	rejectReasonsJSON := chrobinson.ConvertRejectReasonsToString(offerResponse.RejectReasons)
	nowRFC3339 := time.Now().UTC().Format(time.RFC3339Nano)
	record, found := runtimeStore.offerByRequestID(offerResponse.OfferRequestId)
	if !found {
		record = chrobinson.OfferResponse{
			CreatedAt: nowRFC3339,
		}
	}
	record.LoadNumber = offerResponse.LoadNumber.Int()
	record.CarrierCode = offerResponse.CarrierCode
	record.OfferRequestId = offerResponse.OfferRequestId
	record.OfferId = offerResponse.OfferId.String()
	record.OfferResult = offerResponse.OfferResult
	record.Price = offerResponse.Price.Int()
	record.CurrencyCode = offerResponse.CurrencyCode
	record.RejectReasons = offerResponse.RejectReasons
	record.RejectReasonsStr = rejectReasonsJSON
	record.Status = newStatus
	record.RawPayload = string(rawBody)
	record.UpdatedAt = nowRFC3339
	record = runtimeStore.upsertOffer(record)

	if record.OrderBidID > 0 {
		if record.BrokerResponseAt != "" {
			logrus.WithFields(logrus.Fields{
				"offerRequestId":   offerResponse.OfferRequestId,
				"orderBidId":       record.OrderBidID,
				"brokerResponseAt": record.BrokerResponseAt,
			}).Info("Broker response already forwarded to Loader API")
		} else {
			loaderClient := loader.NewCoreAPIClientFromEnv(nil)
			forwardErr := loaderClient.CreateBrokerResponse(loader.BrokerResponse{
				OrderBidID:  record.OrderBidID,
				OfferResult: offerResponse.OfferResult,
				Price:       offerResponse.Price.Int(),
				TNumber:     record.CarrierCode,
			})
			if forwardErr != nil {
				record.BrokerResponseError = forwardErr.Error()
				record.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
				runtimeStore.upsertOffer(record)
				logrus.WithError(forwardErr).WithFields(logrus.Fields{
					"offerRequestId": offerResponse.OfferRequestId,
					"orderBidId":     record.OrderBidID,
					"offerResult":    offerResponse.OfferResult,
					"price":          offerResponse.Price.Int(),
				}).Error("Failed to forward broker response to Loader API")
				return c.Status(fiber.StatusBadGateway).SendString("failed to forward broker response")
			}

			record.BrokerResponseAt = time.Now().UTC().Format(time.RFC3339Nano)
			record.BrokerResponseError = ""
			record.UpdatedAt = record.BrokerResponseAt
			runtimeStore.upsertOffer(record)
		}
	} else {
		logrus.WithFields(logrus.Fields{
			"offerRequestId": offerResponse.OfferRequestId,
			"loadNumber":     offerResponse.LoadNumber.Int(),
		}).Warn("Offer response received without stored order_bid_id; Loader broker response not forwarded")
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"status": "ok"})
}

// ShipmentDetailsHandler handles the callback for shipment details.
func ShipmentDetailsHandler(c *fiber.Ctx) error {
	rawBody := append([]byte(nil), c.Body()...)
	var shipmentDetails chrobinson.ShipmentDetailsCallback
	if err := json.Unmarshal(rawBody, &shipmentDetails); err != nil {
		log.WithError(err).Error("Failed to parse shipment details")
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid shipment details data",
		})
	}

	log.WithFields(log.Fields{
		"loadNumber":   shipmentDetails.LoadNumber.String(),
		"carrierCode":  shipmentDetails.CarrierCode,
		"scac":         shipmentDetails.Scac,
		"clientId":     shipmentDetails.ClientId,
		"eventType":    shipmentDetails.Event.EventType,
		"eventSubType": shipmentDetails.Event.EventSubType,
		"mode":         shipmentDetails.Event.Mode,
	}).Info("Received shipment details callback")

	runtimeStore.addShipmentDetails(chrobinson.ShipmentDetailsRecord{
		LoadNumber:   shipmentDetails.LoadNumber.String(),
		CarrierCode:  shipmentDetails.CarrierCode,
		Scac:         shipmentDetails.Scac,
		ClientID:     shipmentDetails.ClientId,
		CallbackTime: shipmentDetails.Time,
		EventTime:    shipmentDetails.EventTime,
		EventType:    shipmentDetails.Event.EventType,
		EventSubType: shipmentDetails.Event.EventSubType,
		Mode:         shipmentDetails.Event.Mode,
		ActivityDate: shipmentDetails.Event.ActivityDate,
		RawPayload:   string(rawBody),
	})

	// Forward the raw callback to the Loader, tagged with the order_bid_id we
	// captured when this load was booked, so the colleague can attach shipment
	// details to the originating bid row. If we have no matching booking (e.g.
	// the load was booked outside this process or before a restart), we skip the
	// forward rather than fail the callback — CHRob still gets its 200.
	if loadNum, convErr := strconv.Atoi(shipmentDetails.LoadNumber.String()); convErr == nil {
		if orderBidID, ok := runtimeStore.orderBidIDForLoad(loadNum); ok {
			loaderClient := loader.NewCoreAPIClientFromEnv(nil)
			fwdErr := loaderClient.CreateShipmentDetailsForward(loader.ShipmentDetailsForward{
				OrderBidID: orderBidID,
				LoadNumber: shipmentDetails.LoadNumber.String(),
				TNumber:    shipmentDetails.CarrierCode,
				Callback:   json.RawMessage(rawBody),
			})
			if fwdErr != nil {
				logrus.WithError(fwdErr).WithFields(logrus.Fields{
					"loadNumber": shipmentDetails.LoadNumber.String(),
					"orderBidId": orderBidID,
				}).Error("Failed to forward shipment details to Loader API")
			}
		} else {
			logrus.WithField("loadNumber", shipmentDetails.LoadNumber.String()).
				Info("Shipment details received with no stored order_bid_id; not forwarded to Loader")
		}
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"status": "ok"})
}

// Handles the route for submitting a load offer.
func SubmitLoadOfferHandler(apiClient *chrobinson.APIClient) fiber.Handler {
	return func(c *fiber.Ctx) error {
		loadNumber := c.Params("loadNumber")
		parsedLoadNumber, parseErr := strconv.Atoi(loadNumber)
		if parseErr != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "loadNumber must be an integer",
			})
		}
		parsedInput, err := validateAndBuildOfferRequest(c.Body())
		if err != nil {
			log.WithError(err).Error("Failed to parse offer request body")
			if fe, ok := err.(*fiber.Error); ok {
				return c.Status(fe.Code).JSON(fiber.Map{"error": fe.Message})
			}
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request data"})
		}
		offerRequest := parsedInput.Request

		if offerRequest.CarrierCode == "" {
			offerRequest.CarrierCode = config.GetEnv(config.CHRobCarrierCode, "")
		}
		if loadNumber == "" || offerRequest.CarrierCode == "" || offerRequest.OfferPrice <= 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "loadNumber, carrierCode, and offerPrice are required",
			})
		}

		idempotencyKey, err := idempotencyKeyFromRequest(c)
		if err != nil {
			if fe, ok := err.(*fiber.Error); ok {
				return c.Status(fe.Code).JSON(fiber.Map{"error": fe.Message})
			}
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid Idempotency-Key"})
		}
		fingerprint := offerSubmitFingerprint(loadNumber, offerRequest)
		if idempotencyKey != "" {
			if cached, hit, conflict := offerSubmitIdempotency.Get(idempotencyKey, fingerprint, time.Now()); conflict {
				return c.Status(fiber.StatusConflict).JSON(fiber.Map{
					"error": "Idempotency-Key was already used with a different request payload",
				})
			} else if hit {
				c.Set("X-Idempotent-Replay", "true")
				return c.Status(fiber.StatusAccepted).JSON(cached)
			}
		}

		var submitResponse *chrobinson.LoadOfferSubmitResponse
		err = chrobinson.HandleAPICall(apiClient, func() error {
			var err error
			submitResponse, err = apiClient.SubmitLoadOffer(loadNumber, offerRequest)
			return err
		})

		if err != nil {
			fields := log.Fields{
				"loadNumber":  loadNumber,
				"carrierCode": offerRequest.CarrierCode,
				"offerPrice":  offerRequest.OfferPrice,
				"chrobStatus": chrobinson.ErrorStatusCode(err),
			}
			if parsed, ok := chrobinson.ParseAPIErrorSchemaFromError(err); ok {
				fields["chrobStatusCode"] = parsed.StatusCode
				fields["chrobError"] = parsed.Error
				fields["chrobMessage"] = parsed.Message
			}
			log.WithError(err).WithFields(fields).Error("Failed to submit load offer")
			status, body := chrobOfferSubmitErrorResponse(err)
			return c.Status(status).JSON(body)
		}

		if submitResponse == nil {
			submitResponse = &chrobinson.LoadOfferSubmitResponse{}
		}

		persisted := false
		persistWarning := ""
		nowRFC3339 := time.Now().UTC().Format(time.RFC3339Nano)
		if submitResponse.OfferRequestId == "" {
			persistWarning = "CHRob accepted the offer but did not return an offerRequestId; local tracking skipped"
			log.WithFields(log.Fields{
				"loadNumber":   loadNumber,
				"carrierCode":  offerRequest.CarrierCode,
				"offerPrice":   offerRequest.OfferPrice,
				"currencyCode": offerRequest.CurrencyCode,
			}).Warn("Load offer accepted without offerRequestId")
		} else {
			runtimeStore.upsertOffer(chrobinson.OfferResponse{
				LoadNumber:       parsedLoadNumber,
				CarrierCode:      offerRequest.CarrierCode,
				OfferRequestId:   submitResponse.OfferRequestId,
				OrderBidID:       parsedInput.OrderBidID,
				Price:            offerRequest.OfferPrice,
				CurrencyCode:     offerRequest.CurrencyCode,
				RejectReasons:    []string{},
				RejectReasonsStr: chrobinson.ConvertRejectReasonsToString([]string{}),
				Status:           "pending",
				CreatedAt:        nowRFC3339,
				UpdatedAt:        nowRFC3339,
			})
			persistWarning = "Offer is tracked in memory only and will reset on restart"
		}

		log.WithFields(log.Fields{
			"loadNumber":     loadNumber,
			"carrierCode":    offerRequest.CarrierCode,
			"offerPrice":     offerRequest.OfferPrice,
			"offerRequestId": submitResponse.OfferRequestId,
			"persisted":      persisted,
		}).Info("Load offer submission completed")

		response := offerSubmitResponse{
			Message:        "Load offer submitted successfully",
			LoadNumber:     loadNumber,
			TNumber:        offerRequest.CarrierCode,
			OfferRequestID: submitResponse.OfferRequestId,
			Status:         "pending",
			Persisted:      persisted,
			Warning:        persistWarning,
		}
		if idempotencyKey != "" {
			offerSubmitIdempotency.Put(idempotencyKey, fingerprint, response, time.Now())
		}

		return c.Status(fiber.StatusAccepted).JSON(response)
	}
}

// MarkBookedHandler is a convenience endpoint that proxies to CHRob booking.
func MarkBookedHandler(apiClient *chrobinson.APIClient) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var bookingRequest chrobinson.LoadBookingRequest
		if err := c.BodyParser(&bookingRequest); err != nil {
			log.WithError(err).Error("Failed to parse booking request body")
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid request data",
			})
		}
		if bookingRequest.CarrierCode == "" {
			bookingRequest.CarrierCode = config.GetEnv(config.CHRobCarrierCode, "")
		}
		if bookingRequest.LoadNumber == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "loadNumber is required",
			})
		}
		if len(bookingRequest.AvailableLoadCosts) == 0 {
			if err := populateBookingRequestFromOffer(&bookingRequest); err != nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": err.Error(),
				})
			}
		}
		if bookingRequest.CarrierCode == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "carrierCode is required",
			})
		}
		if len(bookingRequest.AvailableLoadCosts) == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "availableLoadCosts must include at least one item",
			})
		}

		err := chrobinson.HandleAPICall(apiClient, func() error {
			return apiClient.BookLoad(bookingRequest)
		})
		if err != nil {
			log.WithError(err).Error("Failed to mark load booked")
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to mark load booked",
			})
		}

		return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
			"message": "Load marked as booked",
		})
	}
}

func populateBookingRequestFromOffer(bookingRequest *chrobinson.LoadBookingRequest) error {
	if bookingRequest == nil || bookingRequest.LoadNumber == 0 {
		return nil
	}
	if len(bookingRequest.AvailableLoadCosts) > 0 {
		return nil
	}

	// Preferred path: derive costs from a prior accepted/countered offer for
	// this load. If there's no such offer (e.g. a direct book without bidding
	// first, or the offer was lost on restart), fall back to the search cost
	// cache — costs are cached for every load we search, independent of offers.
	if offer, ok := runtimeStore.latestOfferByLoadNumber(bookingRequest.LoadNumber); ok {
		if bookingRequest.CarrierCode == "" && offer.CarrierCode != "" {
			bookingRequest.CarrierCode = offer.CarrierCode
		}
		if offer.Status != "booked" && offer.Status != "countered" {
			return fmt.Errorf("cannot derive booking cost for loadNumber %d without an accepted or countered offer response", bookingRequest.LoadNumber)
		}
	}

	loadCosts, ok := chrobinson.BookingLoadCostsForLoadNumber(bookingRequest.LoadNumber)
	if !ok || len(loadCosts) == 0 {
		return fmt.Errorf("no cached availableLoadCosts found for loadNumber %d; provide availableLoadCosts in the request or retry after the load has been searched/ingested", bookingRequest.LoadNumber)
	}

	bookingRequest.AvailableLoadCosts = loadCosts

	log.WithFields(log.Fields{
		"loadNumber":  bookingRequest.LoadNumber,
		"carrierCode": bookingRequest.CarrierCode,
		"costCount":   len(loadCosts),
	}).Info("Derived booking availableLoadCosts from cached CHRob shipment data")

	return nil
}

// DocumentUploadHandler handles uploading documents to C.H. Robinson.
func DocumentUploadHandler(apiClient *chrobinson.APIClient) fiber.Handler {
	return func(c *fiber.Ctx) error {
		loadNumber := c.Params("loadNumber")

		// Retrieve the file from the form data
		fileHeader, err := c.FormFile("file")
		if err != nil {
			log.WithError(err).Error("Failed to retrieve the file")
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid or missing file",
			})
		}

		docType := c.FormValue("docType")
		if docType == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Document type is required",
			})
		}

		// Assuming you adjusted your UploadDocument to accept *fiber.File
		err = apiClient.UploadDocument(loadNumber, fileHeader, docType)
		if err != nil {
			log.WithError(err).Error("Failed to upload document")
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to upload document",
			})
		}

		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"message": "Document uploaded successfully",
		})
	}
}

// firstNonEmptyBooking returns the first non-empty trimmed string, or "".
func firstNonEmptyBooking(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// applyBookingDefaults fills the CHRob-required logistics fields
// (emptyLocation, emptyDateTime, rateConfirmation) from the load's cached
// pickup origin when the caller omitted them. Callers that DO supply these
// fields keep their values — we only fill blanks. This lets the Loader book
// with just load/carrier/bid ids, defaulting the truck's empty position to the
// pickup location on the pickup date.
func applyBookingDefaults(req *chrobinson.LoadBookingRequest) {
	if req == nil || req.LoadNumber == 0 {
		return
	}
	defaults, ok := chrobinson.PickupDefaultsForLoadNumber(req.LoadNumber)
	if !ok {
		return
	}

	emptyLocationBlank := req.EmptyLocation.City == "" &&
		req.EmptyLocation.Coordinate.Latitude == 0 &&
		req.EmptyLocation.Coordinate.Longitude == 0
	if emptyLocationBlank {
		o := defaults.Origin
		req.EmptyLocation = chrobinson.BookingLocation{
			City:    firstNonEmptyBooking(o.City),
			State:   firstNonEmptyBooking(o.State, o.StateCode),
			Zip:     firstNonEmptyBooking(o.Zip, o.PostalCode),
			Country: firstNonEmptyBooking(o.Country, o.CountryCode),
			County:  o.County,
			Coordinate: chrobinson.BookingCoordinate{
				Latitude:  o.Coordinate.Lat,
				Longitude: o.Coordinate.Lon,
			},
		}
	}

	if strings.TrimSpace(req.EmptyDateTime) == "" && defaults.PickupDateTime != "" {
		req.EmptyDateTime = defaults.PickupDateTime
	}

	if strings.TrimSpace(req.RateConfirmation.Name) == "" {
		req.RateConfirmation.Name = firstNonEmptyBooking(
			config.GetEnv("BOOKING_RATECON_NAME", ""), "HField Dispatch")
	}
	if strings.TrimSpace(req.RateConfirmation.Email) == "" {
		req.RateConfirmation.Email = firstNonEmptyBooking(
			config.GetEnv("BOOKING_RATECON_EMAIL", ""), "dispatch@hfield.net")
	}
}
