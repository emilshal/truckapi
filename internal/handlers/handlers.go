package handlers

import (
	"strconv"
	"strings"
	"time"
	"truckapi/db"
	"truckapi/internal/chrobinson"
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
			// Before sending the response
			log.Infof("Sending search response: %+v", searchResponse)
			return nil
		})

		// If there's an error, log it and return an appropriate HTTP status code and error message.
		if err != nil {
			log.WithError(err).Error("Failed to search for available shipments")
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}

		// Save the shipment data to the database
		// Prototype: skip persisting loads unless explicitly enabled.
		// Set `SAVE_SHIPMENTS_TO_DB=true` to restore the old behavior.
		if v := strings.ToLower(strings.TrimSpace(config.GetEnv("SAVE_SHIPMENTS_TO_DB", "false"))); v == "1" || v == "true" || v == "yes" {
			for _, shipment := range searchResponse.Results {
				if saveErr := chrobinson.SaveLoadToDB(db.DB, shipment); saveErr != nil {
					log.WithError(saveErr).Error("Failed to save shipment to DB")
					return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": saveErr.Error()})
				}
			}
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

		if bookingRequest.CarrierCode == "" {
			bookingRequest.CarrierCode = config.GetEnv(config.CHRobCarrierCode, "")
		}
		if bookingRequest.LoadNumber == 0 || bookingRequest.CarrierCode == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "loadNumber and carrierCode are required",
			})
		}
		if len(bookingRequest.AvailableLoadCosts) == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "availableLoadCosts must include at least one item",
			})
		}

		// Use HandleAPICall to make the API call and handle token refresh if needed.
		err := chrobinson.HandleAPICall(apiClient, func() error {
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

		// If everything was successful, return an appropriate response
		return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
			"message": "Load booked successfully",
		})
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

	// Save the offer to the database
	if err := db.DB.Create(&offer).Error; err != nil {
		log.WithError(err).Error("Failed to save offer load to the database")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to save offer load to the database",
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Offer load saved successfully",
		"offerId": offer.ID,
	})
}

// FetchAllOffersHandler handles fetching all offer responses.
func FetchAllOffersHandler(c *fiber.Ctx) error {
	var offers []chrobinson.OfferResponse

	if err := db.DB.Find(&offers).Error; err != nil {
		log.Println("Failed to fetch offers from the database:", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch offers from the database",
		})
	}

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

// OfferResponseHandler handles the callback for offer responses.

// OfferResponseHandler handles the callback for offer responses.
func OfferResponseHandler(c *fiber.Ctx) error {
	var offerResponse chrobinson.OfferResponse
	if err := c.BodyParser(&offerResponse); err != nil {
		logrus.WithError(err).Error("Failed to parse offer response")
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid offer response data",
		})
	}

	logrus.Infof("Received offer response: %+v", offerResponse)
	if offerResponse.OfferRequestId == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "offerRequestId is required",
		})
	}
	if db.DB == nil {
		logrus.Error("SQLite database is not initialized for offer response callback")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Database is not initialized",
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
	record := chrobinson.OfferResponse{OfferRequestId: offerResponse.OfferRequestId}
	if err := db.DB.
		Where(chrobinson.OfferResponse{OfferRequestId: offerResponse.OfferRequestId}).
		Assign(map[string]interface{}{
			"load_number":      offerResponse.LoadNumber,
			"carrier_code":     offerResponse.CarrierCode,
			"offer_id":         offerResponse.OfferId,
			"offer_result":     offerResponse.OfferResult,
			"price":            offerResponse.Price,
			"currency_code":    offerResponse.CurrencyCode,
			"reject_reasons":   rejectReasonsJSON,
			"status":           newStatus,
			"offer_request_id": offerResponse.OfferRequestId,
		}).
		FirstOrCreate(&record).Error; err != nil {
		logrus.WithError(err).Error("Failed to update offer status in the database")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to update offer status in the database",
		})
	}

	c.Set(fiber.HeaderContentType, "text/plain; charset=utf-8")
	return c.Status(fiber.StatusOK).SendString("ok")
}

// ShipmentDetailsHandler handles the callback for shipment details.
func ShipmentDetailsHandler(c *fiber.Ctx) error {
	var shipmentDetails chrobinson.ShipmentDetails
	if err := c.BodyParser(&shipmentDetails); err != nil {
		log.WithError(err).Error("Failed to parse shipment details")
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid shipment details data",
		})
	}

	log.Infof("Received shipment details: %+v", shipmentDetails)

	// Process the shipment details accordingly
	// For example, update your database, notify stakeholders, etc.
	// Implementation of this depends on your business logic.

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Shipment details received successfully",
	})
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
		offerRequest, err := validateAndBuildOfferRequest(c.Body())
		if err != nil {
			log.WithError(err).Error("Failed to parse offer request body")
			if fe, ok := err.(*fiber.Error); ok {
				return c.Status(fe.Code).JSON(fiber.Map{"error": fe.Message})
			}
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request data"})
		}

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
		if submitResponse.OfferRequestId == "" {
			persistWarning = "CHRob accepted the offer but did not return an offerRequestId; local tracking skipped"
			log.WithFields(log.Fields{
				"loadNumber":   loadNumber,
				"carrierCode":  offerRequest.CarrierCode,
				"offerPrice":   offerRequest.OfferPrice,
				"currencyCode": offerRequest.CurrencyCode,
			}).Warn("Load offer accepted without offerRequestId")
		} else if db.DB == nil {
			persistWarning = "CHRob accepted the offer but local database is not initialized; tracking skipped"
			log.WithField("offerRequestId", submitResponse.OfferRequestId).Error("SQLite database is not initialized for load offer persistence")
		} else {
			record := chrobinson.OfferResponse{OfferRequestId: submitResponse.OfferRequestId}
			if err := db.DB.
				Where(chrobinson.OfferResponse{OfferRequestId: submitResponse.OfferRequestId}).
				Assign(map[string]interface{}{
					"load_number":      parsedLoadNumber,
					"carrier_code":     offerRequest.CarrierCode,
					"price":            offerRequest.OfferPrice,
					"currency_code":    offerRequest.CurrencyCode,
					"status":           "pending",
					"reject_reasons":   chrobinson.ConvertRejectReasonsToString([]string{}),
					"offer_request_id": submitResponse.OfferRequestId,
				}).
				FirstOrCreate(&record).Error; err != nil {
				persistWarning = "Offer was sent to CHRob but failed to save locally"
				log.WithError(err).WithField("offerRequestId", submitResponse.OfferRequestId).Error("Failed to persist submitted load offer")
			} else {
				persisted = true
			}
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
		if bookingRequest.LoadNumber == 0 || bookingRequest.CarrierCode == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "loadNumber and carrierCode are required",
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
