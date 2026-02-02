package chrobinson

import (
	"time"

	log "github.com/sirupsen/logrus"
)

// StartMilestoneUpdater initializes a goroutine that periodically sends milestone updates to the C.H. Robinson API.
func StartMilestoneUpdater(client *APIClient) {
	// Start a separate goroutine to allow the milestone updates to run independently of the main application flow.
	go func() {
		// Create a ticker that triggers every 30 minutes.
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop() // Ensure the ticker is cleaned up properly when the function exits.

		// Function that constructs and sends a milestone update.
		sendMilestoneUpdate := func() {
			// Prepare the MilestoneUpdate struct with the necessary data.
			update := MilestoneUpdate{
				EventCode: "X3",
				ShipmentIdentifier: ShipmentIdentifier{
					ShipmentNumber: "123456789",
					ProNumber:      "PRO512461",
				},
				DateTime: DateTime{
					EventDateTime: time.Now().Format(time.RFC3339),
				},
				Carrier: &Carrier{
					Name:                     "Test Carrier",
					DotNumber:                "Test",
					McNumber:                 "MC Number",
					Scac:                     "RBTW",
					Temperature:              32,
					TemperatureUnitOfMeasure: "Fahrenheit",
					VehicleDetail: VehicleDetail{
						TractorNumber:     "375",
						TrailerNumber:     "PV5346",
						LicensePlate:      "MCV246",
						LicensePlateState: "MN",
						Vin:               "1HGBH41JXMN109186",
						DriverContactInformation: DriverContactInformation{
							DriverName:        "Red Barkley",
							DriverPhone:       "5556129457",
							SecondDriverName:  "John Mann",
							SecondDriverPhone: "7038885585",
							DispatchName:      "Tiger",
							DispatchPhone:     "7778889235",
						},
					},
				},
				Location: MilestoneLocation{
					Type:               "D",
					StopSequenceNumber: 2,
					SequenceNumber:     1,
					Name:               "Bulk Goods Warehouse",
					LocationId:         "W4875164",
					Address: Address{
						Address1:          "14800 Charlson Rd",
						Address2:          "Building 1",
						City:              "Eden Prairie",
						Latitude:          44.820538,
						Longitude:         -93.464427,
						StateProvinceCode: "MN",
						PostalCode:        "55347",
						Country:           "US",
					},
				},
				Items: []Item{
					{
						ItemId:              "123456",
						Weight:              45,
						WeightUnitOfMeasure: "Pounds",
						Quantity:            12,
						Pallets:             1,
					},
				},
				SupplementalInformation: &SupplementalInformation{
					ReasonCode: "1",
					Notes:      "Truck broke its axle",
				},
			}

			// Define the API call for sending the milestone update.
			apiCall := func() error {
				return client.UpdateMilestone(update)
			}

			// Use the HandleAPICall function to attempt the API call with error handling.
			if err := HandleAPICall(client, apiCall); err != nil {
				log.WithError(err).Error("Failed to send milestone update")
			} else {
				log.Info("Milestone update sent successfully")
			}
		}
		// Send the first update immediately.
		sendMilestoneUpdate()

		// Continue sending updates every 30 minutes as per the ticker.
		for range ticker.C {
			sendMilestoneUpdate()
		}
	}()
}
