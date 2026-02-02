package chrobinson

import (
	"regexp"
	"strings"
)

func ParseAddress(address string) Location {
	parts := strings.Split(address, ",")
	if len(parts) < 3 {
		return Location{} // or handle the error according to your needs
	}

	country := strings.TrimSpace(parts[len(parts)-1])  // Last element is country
	stateZip := strings.TrimSpace(parts[len(parts)-2]) // Second to last is state and zip

	city := strings.TrimSpace(parts[len(parts)-3]) // Third to last might be city or street
	if len(parts) > 3 {
		city = strings.TrimSpace(parts[len(parts)-3]) // If there's a street address, city is at -3
	}

	// Extract state and zip from stateZip part
	re := regexp.MustCompile(`^(.*?)(?:\s+(\d+))?$`)
	matches := re.FindStringSubmatch(stateZip)
	state, zip := "", ""
	if len(matches) > 1 {
		state = matches[1]
	}
	if len(matches) > 2 {
		zip = matches[2]
	}

	return Location{
		City:    city,
		Country: country,
		Zip:     zip,
		State:   state,
	}
}

// func CreateSearchPayload(truck Truck, pseudoLoc PseudoLocations) AvailableShipmentSearchRequest {
// 	payload := AvailableShipmentSearchRequest{
// 		PageIndex:  0,
// 		PageSize:   100,
// 		RegionCode: "NA",
// 		Modes:      []string{"V", "R"},
// 	}

// 	payload.RegionCode = "US"
// 	payload.OriginRadiusSearch.Coordinate.Lat = pseudoLoc.Lat
// 	payload.OriginRadiusSearch.Coordinate.Lon = pseudoLoc.Lng
// 	payload.OriginRadiusSearch.Radius.Value = truck.Radius
// 	payload.OriginRadiusSearch.Radius.UnitOfMeasure = "Standard"
// 	payload.TeamLoad = truck.IsTeam
// 	payload.EquipmentLengthRange.UnitOfMeasure = "Standard"
// 	payload.EquipmentLengthRange.Min = 0
// 	payload.EquipmentLengthRange.Max = truck.Length
// 	payload.CarrierCode = config.GetEnv(config.CHRobCarrierCode, "")
// 	payload.LoadWeightRange.UnitOfMeasure = "Standard"
// 	payload.LoadWeightRange.Min = 0
// 	payload.LoadWeightRange.Max = float64(truck.Weight)

// 	// Populate other fields similarly...

// 	return payload
// }

// func CreateLoadOffer() LoadOfferRequest {
// 	offer := LoadOfferRequest{
// 		CarrierCode:  config.GetEnv(config.CHRobCarrierCode, ""),
// 		OfferPrice:   2000,
// 		OfferNote:    "My note",
// 		CurrencyCode: "USD",
// 	}
// 	return offer
// }

// func CreateBookRequest(truck Truck, search SearchResponse) LoadBookingRequest {
// 	booking := LoadBookingRequest{
// 		LoadNumber: ,
// 	}

// 	return booking
// // }

// func FetchTrucks(db *gorm.DB) ([]Truck, error) {
// 	var trucks []Truck
// 	result := db.Find(&trucks)
// 	if result.Error != nil {
// 		log.WithError(result.Error).Error("Error fetching trucks")
// 		return nil, result.Error
// 	}
// 	log.Info("Trucks fetched successfully")
// 	return trucks, nil
// }

// // GetTrucksByCompanyName fetches all trucks matching a specific company name
// func GetTrucksByCompanyName(db *gorm.DB, companyName string) ([]Truck, error) {
// 	var trucks []Truck
// 	result := db.Where("company_name = ?", companyName).Find(&trucks)
// 	if result.Error != nil {
// 		log.WithError(result.Error).Error("Error fetching trucks by company name")
// 		return nil, result.Error
// 	}
// 	log.Infof("Fetched trucks for company: %s", companyName)
// 	return trucks, nil
// }

// // GetTruckByID fetches a single truck by its ID
// func GetTruckByID(db *gorm.DB, id int) (*Truck, error) {
// 	var truck Truck
// 	result := db.First(&truck, id) // GORM automatically uses the primary key for the First method
// 	if result.Error != nil {
// 		log.WithError(result.Error).Error("Error fetching truck by ID")
// 		return nil, result.Error
// 	}
// 	log.Infof("Fetched truck ID: %d", id)
// 	return &truck, nil
// }

// // Function to map ShipmentResponse to Order and save to DB
// func SaveShipmentToDB(shipment ShipmentInfo) error {

// 	// Parse date fields
// 	pickupDate, err := time.Parse(time.RFC3339, shipment.ReadyBy)
// 	if err != nil {
// 		return err
// 	}
// 	deliveryDate, err := time.Parse(time.RFC3339, shipment.DeliverBy)
// 	if err != nil {
// 		return err
// 	}
// 	// Convert LoadNumber to string
// 	orderNumber := strconv.Itoa(shipment.LoadNumber)

// 	// Create Order struct
// 	order := Order{
// 		OrderNumber:       orderNumber, // Assuming LoadNumber can be used as OrderNumber
// 		PickupLocation:    shipment.Origin.City + ", " + shipment.Origin.State,
// 		DeliveryLocation:  shipment.Destination.City + ", " + shipment.Destination.State,
// 		PickupDate:        pickupDate,
// 		LocalPickupDate:   pickupDate,             // Assuming local pickup date is the same as pickup date
// 		PickupAsap:        shipment.HasDriverWork, // Adjust as needed based on your logic
// 		DeliveryDate:      deliveryDate,
// 		DockLevel:         false, // Placeholder: adjust based on your logic
// 		DeliveryZip:       shipment.Destination.Zip,
// 		Pays:              int(shipment.AvailableLoadCosts[0].SourceCostPerUnit * float64(shipment.AvailableLoadCosts[0].Units)), //confirm this
// 		PaysRate:          shipment.AvailableLoadCosts[0].SourceCostPerUnit,
// 		TruckTypeID:       0,  // Placeholder: map appropriately
// 		Link:              "", // Placeholder: adjust based on your logic
// 		OrderTypeID:       0,  // Placeholder: adjust based on your logic
// 		ExternalLink:      shipment.Comment,
// 		LiftGate:          false, //?
// 		OriginalTruckSize: "",    //?
// 		Extra:             "",    // Placeholder: adjust based on your logic
// 		Shipper:           shipment.Contact.Name,
// 		Receiver:          "", // Placeholder: adjust based on your logic
// 		SelectedTrucks:    0,  // Placeholder: adjust based on your logic
// 		SentTrucks:        0,  // Placeholder: adjust based on your logic
// 	}

// 	// // Save to database
// 	// if err := db.Save(&order).Error; err != nil {
// 	//     return err
// 	// }

// 	return nil
// }
