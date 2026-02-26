package chrobinson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"time"
	"truckapi/internal/auth"
	"truckapi/internal/httpdebug"

	"github.com/gofiber/fiber/v2"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type APIClient struct {
	BaseURL    string
	TokenStore *auth.TokenStore
	HTTPClient *http.Client
}

func NewAPIClient(baseURL string, tokenStore *auth.TokenStore, httpClient *http.Client) *APIClient {
	if httpClient == nil {
		httpClient = &http.Client{} // Use default client if none provided
	}
	if httpClient.Transport == nil {
		httpClient.Transport = httpdebug.NewTransport(http.DefaultTransport)
	}
	return &APIClient{
		BaseURL:    baseURL,
		TokenStore: tokenStore,
		HTTPClient: httpClient,
	}
}

func HandleAPICall(client *APIClient, apiCall func() error) error {
	err := apiCall()
	if err != nil {
		// Check if the error is a fiber.Error and has a StatusUnauthorized code
		if fiberErr, ok := err.(*fiber.Error); ok && fiberErr.Code == fiber.StatusUnauthorized {
			// Token might be expired, try refreshing it
			if refreshErr := client.TokenStore.RefreshToken(); refreshErr != nil {
				log.WithError(refreshErr).Error("Failed to refresh token")
				return refreshErr
			} else {
				// Retry the API call with the refreshed token
				err = apiCall()
				if err != nil {
					log.WithError(err).Error("Failed to make API call after token refresh")
					return err
				}
			}
		} else {
			log.WithError(err).Error("Failed to make API call")
			return err
		}
	}
	return nil
}

// Event Retrieval
func (client *APIClient) GetEvents(from, to time.Time, queryParameters map[string]string) (*EventResponse, error) {
	// Construct the request URL with query parameters
	url := fmt.Sprintf("%s/v2/events?from=%s&to=%s", client.BaseURL, from.Format(time.RFC3339), to.Format(time.RFC3339))
	for key, value := range queryParameters {
		url += fmt.Sprintf("&%s=%s", key, value)
	}
	// Get the token from the token store
	token, err := client.TokenStore.GetValidToken()
	if err != nil {
		return nil, err
	}

	// Create the HTTP request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	// Make the HTTP request
	resp, err := client.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Check for a successful response
	if resp.StatusCode != http.StatusOK {
		return nil, fiber.NewError(resp.StatusCode, fmt.Sprintf("API request failed with status code: %d", resp.StatusCode))
	}

	// Parse the response body
	var eventResponse EventResponse
	if err := json.NewDecoder(resp.Body).Decode(&eventResponse); err != nil {
		return nil, err
	}

	return &eventResponse, nil
}

// ConvertRejectReasonsToString converts a slice of strings to a JSON string.
func ConvertRejectReasonsToString(reasons []string) string {
	data, _ := json.Marshal(reasons)
	return string(data)
}

// ConvertRejectReasonsToSlice converts a JSON string to a slice of strings.
func ConvertRejectReasonsToSlice(reasons string) ([]string, error) {
	var result []string
	err := json.Unmarshal([]byte(reasons), &result)
	return result, err
}

// SearchAvailableShipments makes a POST request to the C.H. Robinson API to search for available shipments.
func (client *APIClient) SearchAvailableShipments(request AvailableShipmentSearchRequest) (*AvailableShipmentSearchResponse, error) {
	url := client.BaseURL + "/v2/shipments/available/searches"

	// Marshal the request body to JSON
	requestBody, err := json.Marshal(request)
	if err != nil {
		log.WithError(err).Error("Failed to marshal request body")
		return nil, err
	}

	// Get the OAuth token from the token store
	token, err := client.TokenStore.GetValidToken()
	if err != nil {
		log.WithError(err).Error("Failed to get valid token")
		return nil, err
	}

	// Create the HTTP request
	httpRequest, err := http.NewRequest("POST", url, bytes.NewBuffer(requestBody))
	if err != nil {
		log.WithError(err).Error("Failed to create HTTP request")
		return nil, err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+token)
	httpRequest.Header.Set("Content-Type", "application/json")

	// Before sending the request
	log.WithFields(log.Fields{
		"url": url,
	}).Debug("Sending CHRob SearchAvailableShipments request")

	// Make the HTTP request
	response, err := client.HTTPClient.Do(httpRequest)
	if err != nil {
		log.WithError(err).Error("Failed to make HTTP request")
		return nil, err
	}
	defer response.Body.Close()

	// After receiving the response
	log.WithFields(log.Fields{
		"url":    url,
		"status": response.StatusCode,
	}).Debug("Received CHRob SearchAvailableShipments response")

	// Check for a successful response
	if response.StatusCode != http.StatusOK {
		responseBodyBytes, _ := ioutil.ReadAll(response.Body)
		log.WithFields(log.Fields{
			"url":    url,
			"status": response.StatusCode,
		}).Errorf("CHRob SearchAvailableShipments failed: %s", string(responseBodyBytes))
		return nil, fiber.NewError(response.StatusCode, "Failed to search for available shipments")
	}

	// Parse the response body. We read bytes first so we can log useful diagnostics
	// if the server returns a truncated payload (e.g., "unexpected EOF").
	responseBodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		log.WithError(err).Error("Failed to read CHRob response body")
		return nil, err
	}

	var searchResponse AvailableShipmentSearchResponse
	if err := json.Unmarshal(responseBodyBytes, &searchResponse); err != nil {
		const maxLog = 4096
		snippet := string(responseBodyBytes)
		truncated := false
		if len(snippet) > maxLog {
			snippet = snippet[:maxLog]
			truncated = true
		}
		log.WithError(err).WithFields(log.Fields{
			"url":          url,
			"status":       response.StatusCode,
			"body_bytes":   len(responseBodyBytes),
			"body_snippet": snippet,
			"truncated":    truncated,
		}).Error("Failed to decode CHRob response body")
		return nil, err
	}

	return &searchResponse, nil
}

// UpdateMilestone sends a milestone update to C.H. Robinson.
func (client *APIClient) UpdateMilestone(update MilestoneUpdate) error {
	url := client.BaseURL + "/v1/shipments/milestones"

	// Marshal the update object to JSON
	requestBody, err := json.Marshal(update)
	if err != nil {
		log.WithError(err).Error("Failed to marshal milestone update")
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to marshal milestone update")
	}

	// Get the OAuth token from the token store
	token, err := client.TokenStore.GetValidToken()
	if err != nil {
		log.WithError(err).Error("Failed to get valid token")
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to get valid token")
	}

	// Create the HTTP request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(requestBody))
	if err != nil {
		log.WithError(err).Error("Failed to create HTTP request")
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to create HTTP request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	// Make the HTTP request
	resp, err := client.HTTPClient.Do(req)
	if err != nil {
		log.WithError(err).Error("Failed to make HTTP request")
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to make HTTP request")
	}
	defer resp.Body.Close()

	// Read the response body
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.WithError(err).Error("Failed to read response body")
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to read response body")
	}

	// Log the response body
	log.Infof("Response from CHRobinson: %s, Status Code: %d", string(responseBody), resp.StatusCode)

	// Handle the response based on the status code
	switch resp.StatusCode {
	case http.StatusCreated:
		return nil
	case http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError:
		log.Errorf("API request failed with status code: %d, response: %s", resp.StatusCode, string(responseBody))
		return fiber.NewError(resp.StatusCode, "API request failed")
	case http.StatusUnauthorized:
		log.Errorf("API request failed with status code: %d, response: %s", resp.StatusCode, string(responseBody))
		return fiber.NewError(fiber.StatusUnauthorized, "API request failed with unauthorized access")
	default:
		log.Errorf("Unexpected status code: %d, response: %s", resp.StatusCode, string(responseBody))
		return fiber.NewError(fiber.StatusInternalServerError, "Unexpected error")
	}

}

func (client *APIClient) BookLoad(bookingRequest LoadBookingRequest) error {
	url := client.BaseURL + "/v1/shipments/books" // Adjust the URL as necessary

	requestBody, err := json.Marshal(bookingRequest)
	if err != nil {
		log.WithError(err).Error("Failed to marshal booking request body")
		return err
	}

	httpRequest, err := http.NewRequest("POST", url, bytes.NewBuffer(requestBody))
	if err != nil {
		log.WithError(err).Error("Failed to create HTTP request for booking load")
		return err
	}

	token, err := client.TokenStore.GetValidToken()
	if err != nil {
		log.WithError(err).Error("Failed to get valid token")
		return err
	}

	httpRequest.Header.Set("Authorization", "Bearer "+token)
	httpRequest.Header.Set("Content-Type", "application/json")

	log.Infof("Sending booking request to URL: %s with body: %s", url, requestBody)
	response, err := client.HTTPClient.Do(httpRequest)
	if err != nil {
		log.WithError(err).Error("Failed to make HTTP request for booking load")
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusAccepted {
		responseBody, errRead := ioutil.ReadAll(response.Body)
		if errRead != nil {
			log.WithError(errRead).Error("Failed to read response body")
		}
		log.Errorf("Failed to book load, status code: %d, response: %s", response.StatusCode, string(responseBody))
		return fmt.Errorf("API request failed with status code %d: %s", response.StatusCode, string(responseBody))
	}

	log.Info("Load booked successfully")
	return nil
}

func (client *APIClient) SubmitLoadOffer(loadNumber string, offerRequest LoadOfferRequest) (*LoadOfferSubmitResponse, error) {
	url := fmt.Sprintf("%s/v1/shipments/%s/offers", client.BaseURL, loadNumber)

	requestBody, err := json.Marshal(offerRequest)
	if err != nil {
		log.WithError(err).Error("Failed to marshal offer request body")
		return nil, err
	}

	httpRequest, err := http.NewRequest("POST", url, bytes.NewBuffer(requestBody))
	if err != nil {
		log.WithError(err).Error("Failed to create HTTP request for submitting load offer")
		return nil, err
	}

	token, err := client.TokenStore.GetValidToken()
	if err != nil {
		log.WithError(err).Error("Failed to get valid token")
		return nil, err
	}

	httpRequest.Header.Set("Authorization", "Bearer "+token)
	httpRequest.Header.Set("Content-Type", "application/json")

	response, err := client.HTTPClient.Do(httpRequest)
	if err != nil {
		log.WithError(err).Error("Failed to make HTTP request for submitting load offer")
		return nil, err
	}
	defer response.Body.Close()

	responseBody, errRead := ioutil.ReadAll(response.Body)
	if errRead != nil {
		log.WithError(errRead).Error("Failed to read offer response body")
		return nil, errRead
	}

	if response.StatusCode != http.StatusAccepted {
		log.Errorf("Failed to submit load offer, status code: %d, response: %s", response.StatusCode, string(responseBody))
		return nil, fmt.Errorf("API request failed with status code %d: %s", response.StatusCode, string(responseBody))
	}

	submitResponse := &LoadOfferSubmitResponse{}
	if len(bytes.TrimSpace(responseBody)) > 0 {
		if err := json.Unmarshal(responseBody, submitResponse); err != nil {
			log.WithError(err).WithField("response_body", string(responseBody)).Error("Failed to parse load offer response")
			return nil, err
		}
	}

	log.WithFields(log.Fields{
		"loadNumber":      loadNumber,
		"offerRequestId":  submitResponse.OfferRequestId,
		"response_status": response.StatusCode,
	}).Info("Load offer submitted successfully")
	return submitResponse, nil
}

func (client *APIClient) UploadDocument(loadNumber string, fileHeader *multipart.FileHeader, docType string) error {
	// Open the file
	file, err := fileHeader.Open()
	if err != nil {
		return err
	}
	defer file.Close()

	url := client.BaseURL + "/v1/documents/" + loadNumber

	// Create a new multipart writer
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)

	// Create a form file part
	part, err := writer.CreateFormFile("file", fileHeader.Filename)
	if err != nil {
		return err
	}

	// Copy the file data to the form file part
	if _, err = io.Copy(part, file); err != nil {
		return err
	}

	// Add the document type to the multipart form
	if err := writer.WriteField("docType", docType); err != nil {
		return err
	}

	// Close the multipart writer to finalize the body
	if err = writer.Close(); err != nil {
		return err
	}

	// Create and configure the HTTP request
	request, err := http.NewRequest("POST", url, body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())

	token, err := client.TokenStore.GetValidToken()
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)

	// Send the request
	response, err := client.HTTPClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	// Check the response status code
	if response.StatusCode != http.StatusCreated {
		responseBody, _ := ioutil.ReadAll(response.Body)
		return fmt.Errorf("failed to upload document, status code: %d, response: %s", response.StatusCode, responseBody)
	}

	return nil
}

func CreateSearchRequestsForTrucks(apiClient *APIClient, trucks []Truck, locations []PseudoLocations) ([]ShipmentInfo, error) {

	var allShipments []ShipmentInfo

	// Create a map of truck locations by TruckId
	locationMap := make(map[int64]PseudoLocations)
	for _, location := range locations {
		locationMap[location.TruckId] = location
	}

	for _, truck := range trucks {
		location, exists := locationMap[truck.Id]
		if !exists {
			continue // Skip trucks without a location
		}

		searchRequest := AvailableShipmentSearchRequest{
			PageIndex:  0,
			PageSize:   100,
			RegionCode: "NA",
			Modes:      []string{"F", "L", "R", "V", "H"},
			OriginRadiusSearch: &RadiusSearch{
				Coordinate: Coordinate{
					Lat: location.Lat,
					Lon: location.Lng,
				},
				Radius: Radius{
					Value:         250,
					UnitOfMeasure: "Standard",
				},
			},
			LoadDistanceRange: &Range{
				UnitOfMeasure: "Standard",
				Min:           0,
				Max:           5000,
			},
			LoadWeightRange: &Range{
				UnitOfMeasure: "Standard",
				Min:           0,
				Max:           48000,
			},
			EquipmentLengthRange: &Range{
				UnitOfMeasure: "Standard",
				Min:           0,
				Max:           53,
			},
			AvailableForPickUpByDateRange: &DateRange{
				Min: time.Now().Format("2006-01-02"),
				Max: time.Now().AddDate(0, 0, 1).Format("2006-01-02"),
			},
			TeamLoad:             true,
			StfLoad:              true,
			HazMatLoad:           true,
			TankerLoad:           true,
			ChemicalSolutionLoad: true,
			HighValueLoad:        true,
			SortCriteria: &SortCriteria{
				Field:     "LoadNumber",
				Direction: "ascending",
			},
		}

		var searchResponse *AvailableShipmentSearchResponse

		err := HandleAPICall(apiClient, func() error {
			response, err := apiClient.SearchAvailableShipments(searchRequest)
			if err != nil {
				return err
			}
			searchResponse = response
			return nil
		})

		if err != nil {
			log.WithError(err).Error("Failed to search for available shipments")
			return nil, err
		}

		allShipments = append(allShipments, searchResponse.Results...)
	}

	return allShipments, nil
}
func SaveLoadToDB(db *gorm.DB, shipment ShipmentInfo) error {
	var existingShipment ShipmentInfo
	if err := db.Where("load_number = ?", shipment.LoadNumber).First(&existingShipment).Error; err == nil {
		// If a record is found, return without error, since we don't want duplicates.
		log.WithField("loadNumber", shipment.LoadNumber).Info("Load already exists in database, skipping insert.")
		return nil
	} else if err != gorm.ErrRecordNotFound {
		// If there's an error other than record not found, return it.
		log.WithError(err).Error("Error checking for existing load in database")
		return err
	}

	// Otherwise, insert the new shipment.
	if err := db.Create(&shipment).Error; err != nil {
		log.WithError(err).Error("Error saving load to database")
		return err
	}
	log.WithField("loadNumber", shipment.LoadNumber).Info("Load saved successfully")
	return nil
}
