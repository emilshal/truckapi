package chrobinson

import (
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// TokenResponse represents the JSON response from the authentication endpoint.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

// Event-related models

// Event represents the structure of the event data sent by C.H. Robinson.
type Event struct {
	Time                    string `json:"time"`
	Customer                string `json:"customer"`
	EventTime               string `json:"eventTime"`
	CustomerReferenceNumber string `json:"customerReferenceNumber"`
	BillToReferenceNumber   string `json:"billToReferenceNumber"`
	Event                   struct {
		EventType   string `json:"eventType"`
		OrderDetail []struct {
			NavisphereTrackingNumber string `json:"navisphereTrackingNumber"`
			NavisphereTrackingLink   string `json:"navisphereTrackingLink"`
			OrderNumber              int    `json:"orderNumber"`
			Condition                string `json:"condition"`
			Services                 []struct {
				Condition    string `json:"condition"`
				Mode         string `json:"mode"`
				Description  string `json:"description"`
				ServiceLevel string `json:"serviceLevel"`
				Locations    []struct {
					ChrLocationId      string `json:"chrLocationId"`
					CustomerLocationId string `json:"customerLocationId"`
					Type               string `json:"type"`
					Style              string `json:"style"`
					Name               string `json:"name"`
					Address            struct {
						Address1          string  `json:"address1"`
						Address2          string  `json:"address2"`
						City              string  `json:"city"`
						StateProvinceCode string  `json:"stateProvinceCode"`
						PostalCode        string  `json:"postalCode"`
						Country           string  `json:"country"`
						Latitude          float64 `json:"latitude"`
						Longitude         float64 `json:"longitude"`
					} `json:"address"`
					ReferenceNumbers []struct {
						Type  string `json:"type"`
						Value string `json:"value"`
					} `json:"referenceNumbers"`
				} `json:"locations"`
				Items []struct {
					PackagingType                  string  `json:"packagingType"`
					Description                    string  `json:"description"`
					ProductCode                    string  `json:"productCode"`
					FreightClass                   string  `json:"freightClass"`
					UpcNumber                      string  `json:"upcNumber"`
					SkuNumber                      string  `json:"skuNumber"`
					PluNumber                      string  `json:"pluNumber"`
					InsuranceValue                 float64 `json:"insuranceValue"`
					MinWeight                      float64 `json:"minWeight"`
					MinWeightUnitOfMeasure         string  `json:"minWeightUnitOfMeasure"`
					MaxWeight                      float64 `json:"maxWeight"`
					MaxWeightUnitOfMeasure         string  `json:"maxWeightUnitOfMeasure"`
					Length                         float64 `json:"length"`
					LengthUnitOfMeasure            string  `json:"lengthUnitOfMeasure"`
					Height                         float64 `json:"height"`
					HeightUnitOfMeasure            string  `json:"heightUnitOfMeasure"`
					Width                          float64 `json:"width"`
					WidthUnitOfMeasure             string  `json:"widthUnitOfMeasure"`
					Volume                         float64 `json:"volume"`
					VolumeUnitOfMeasure            string  `json:"volumeUnitOfMeasure"`
					Pallets                        int     `json:"pallets"`
					PalletSpaces                   int     `json:"palletSpaces"`
					TrailerLengthUsed              float64 `json:"trailerLengthUsed"`
					TrailerLengthUsedUnitOfMeasure string  `json:"trailerLengthUsedUnitOfMeasure"`
					ReferenceNumbers               []struct {
						Type  string `json:"type"`
						Value string `json:"value"`
					} `json:"referenceNumbers"`
				} `json:"items"`
			} `json:"services"`
		} `json:"orderDetail"`
		LoadNumbers      []int `json:"loadNumbers"`
		ReferenceNumbers []struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"referenceNumbers"`
	} `json:"event"`
}

// EventResponse represents the structure of the response from the event retrieval API.
type EventResponse struct {
	TotalCount int     `json:"totalCount"`
	Results    []Event `json:"results"`
}

// ShipmentEvent represents a single event related to a shipment.
type ShipmentEvent struct {
	ID               uuid.UUID `json:"id" gorm:"type:char(36);primary_key"`
	LoadNumber       string    `json:"loadNumber"`
	Origin           string    `json:"origin"`
	OriginState      string    `json:"ostate"`
	Destination      string    `json:"destination"`
	DestinationState string    `json:"dstate"`
	Weight           float64   `json:"weight"`
	Length           float64   `json:"length"`
	DeadHeadDistance float64   `json:"deadHeadDistance"`
	Price            float64   `json:"price"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

func NewShipmentEvent() *ShipmentEvent {
	return &ShipmentEvent{
		ID: uuid.New(),
	}
}

// Load represents a single load in a shipment.
type Load struct {
	gorm.Model
	CustomerReferenceNumber  string
	BillToReferenceNumber    string
	NavisphereTrackingNumber string
	NavisphereTrackingLink   string
	OrderNumber              int
	Condition                string
	Mode                     string
	Description              string
	ServiceLevel             string
	// Add other fields as needed
}

// Mapping of event types to their descriptions
var EventTypeMap map[string]string

func init() {
	EventTypeMap = map[string]string{
		"ORDER CREATED":        "Order has been created",
		"ORDER REJECTED":       "Order was rejected",
		"ORDER UPDATED":        "Order has been updated",
		"ORDER CANCELED":       "Order has been canceled",
		"ORDER COMPLETED":      "Order is complete",
		"LOAD CREATED":         "Load has been created",
		"LOAD BOOKED":          "Load has been booked",
		"LOAD CANCELLED":       "Load has been cancelled",
		"CARRIER ARRIVED":      "Carrier has arrived",
		"CARRIER DEPARTED":     "Carrier has departed",
		"LOAD PICKED UP":       "Load has been picked up",
		"LOAD DELIVERED":       "Load has been delivered",
		"IN TRANSIT":           "In transit",
		"IN TRANSIT TO ORIGIN": "In transit to origin",
		"APPOINTMENT UPDATED":  "Appointment updated",
		"PACKAGE IN TRANSIT":   "Package in transit",
		"PACKAGE PICKED UP":    "Package picked up",
		"PACKAGE DELIVERED":    "Package delivered",
	}
}
func (e *Event) PlatformEventType() string {
	if r, exists := EventTypeMap[e.Event.EventType]; exists {
		return r
	}

	log.Errorf("unknown order type: %s", e.Event.EventType)
	return ""
}

// AvailableShipmentSearchRequest represents the request body for searching available shipments.
//
//	type AvailableShipmentSearchRequest struct {
//		PageIndex                     int           `json:"pageIndex"`
//		PageSize                      int           `json:"pageSize"`
//		RegionCode                    string        `json:"regionCode"`
//		CarrierCode                   string        `json:"carrierCode"`
//		Modes                         []string      `json:"modes"`
//		OriginRadiusSearch            *RadiusSearch `json:"originRadiusSearch,omitempty"`
//		Destinations                  []Destination `json:"destinations,omitempty"`
//		AvailableForPickUpByDateRange *DateRange    `json:"availableForPickUpByDateRange,omitempty"`
//	}
type AvailableShipmentSearchRequest struct {
	PageIndex                     int           `json:"pageIndex"`
	PageSize                      int           `json:"pageSize"`
	RegionCode                    string        `json:"regionCode"`
	CarrierCode                   string        `json:"carrierCode"`
	Modes                         []string      `json:"modes,omitempty"`
	OriginRadiusSearch            *RadiusSearch `json:"originRadiusSearch,omitempty"`
	DestinationRadiusSearch       *RadiusSearch `json:"destinationRadiusSearch,omitempty"`
	Destinations                  []Destination `json:"destinations,omitempty"`
	AvailableForPickUpByDateRange *DateRange    `json:"availableForPickUpByDateRange,omitempty"`
	LoadDistanceRange             *Range        `json:"loadDistanceRange,omitempty"`
	LoadWeightRange               *Range        `json:"loadWeightRange,omitempty"`
	EquipmentLengthRange          *Range        `json:"equipmentLengthRange,omitempty"`
	TeamLoad                      bool          `json:"teamLoad,omitempty"`
	StfLoad                       bool          `json:"stfLoad,omitempty"`
	HazMatLoad                    bool          `json:"hazMatLoad,omitempty"`
	TankerLoad                    bool          `json:"tankerLoad,omitempty"`
	ChemicalSolutionLoad          bool          `json:"chemicalSolutionLoad,omitempty"`
	HighValueLoad                 bool          `json:"highValueLoad,omitempty"`
	SortCriteria                  *SortCriteria `json:"sortCriteria,omitempty"`
}

type Destination struct {
	CountryCode string   `json:"countryCode"`
	StateCodes  []string `json:"stateCodes"`
}

type RadiusSearch struct {
	Coordinate Coordinate `json:"coordinate"`
	Radius     Radius     `json:"radius"`
}

type Coordinate struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type Radius struct {
	UnitOfMeasure string `json:"unitOfMeasure"`
	Value         int    `json:"value"`
}
type Location struct {
	City    string `json:"city"`
	Country string `json:"country"`
	Zip     string `json:"zip"`
	State   string `json:"state"`
	// Lat     float64 `json:"lat"`
	// Lon     float64 `json:"lon"`
}
type Range struct {
	UnitOfMeasure string  `json:"unitOfMeasure"`
	Min           float64 `json:"min"`
	Max           float64 `json:"max"`
}

type DateRange struct {
	Min string `json:"min"`
	Max string `json:"max"`
}

type SortCriteria struct {
	Field     string `json:"field"`
	Direction string `json:"direction"`
}

type AvailableShipmentSearchResponse struct {
	TotalCount int            `json:"totalCount"`
	Results    []ShipmentInfo `json:"results"`
}

type ShipmentInfo struct {
	ID                                    uint                 `gorm:"primaryKey"`
	LoadNumber                            int                  `json:"loadNumber" gorm:"uniqueIndex"`
	Origin                                Location             `json:"origin" gorm:"embedded;embeddedPrefix:origin_"`
	Destination                           Location             `json:"destination" gorm:"embedded;embeddedPrefix:destination_"`
	Distance                              Distance             `json:"distance" gorm:"embedded;embeddedPrefix:distance_"`
	Weight                                Weight               `json:"weight" gorm:"embedded;embeddedPrefix:weight_"`
	ReadyBy                               string               `json:"readyBy"`
	DeliverBy                             string               `json:"deliverBy"`
	Equipment                             Equipment            `json:"equipment" gorm:"embedded;embeddedPrefix:equipment_"`
	SpecializedEquipment                  SpecializedEquipment `json:"specializedEquipment" gorm:"embedded;embeddedPrefix:specializedEquipment_"`
	HasDriverWork                         bool                 `json:"hasDriverWork"`
	StopCount                             int                  `json:"stopCount"`
	CarrierTier                           string               `json:"carrierTier"`
	IsOkToAdvertise                       bool                 `json:"isOkToAdvertise"`
	IsDatOk                               bool                 `json:"isDatOk"`
	IsHazMat                              bool                 `json:"isHazMat"`
	IsTeamLoad                            bool                 `json:"isTeamLoad"`
	IsRegulatedByStf                      bool                 `json:"isRegulatedByStf"`
	IsTankerEndorsementRequired           bool                 `json:"isTankerEndorsementRequired"`
	IsTest                                bool                 `json:"isTest"`
	IsWebDisplayable                      bool                 `json:"isWebDisplayable"`
	SourceUom                             string               `json:"sourceUom"`
	RegionCode                            string               `json:"regionCode"`
	Comment                               string               `json:"comment"`
	Contact                               Contact              `json:"contact" gorm:"embedded;embeddedPrefix:contact_"`
	Rating                                string               `json:"rating"`
	HasDropTrailer                        bool                 `json:"hasDropTrailer"`
	DeliveryAvailableDate                 string               `json:"deliveryAvailableDate"`
	PickUpByDate                          string               `json:"pickUpByDate"`
	ActivityDate                          string               `json:"activityDate"`
	MinimumLogipoints                     float64              `json:"minimumLogipoints"`
	DeadHeadDistance                      float64              `json:"deadHeadDistance"`
	ReeferSetting                         string               `json:"reeferSetting"`
	ReeferTemperature                     Temperature          `json:"reeferTemperature" gorm:"embedded;embeddedPrefix:reeferTemperature_"`
	MaxTemp                               Temperature          `json:"maxTemp" gorm:"embedded;embeddedPrefix:maxTemp_"`
	MinTemp                               Temperature          `json:"minTemp" gorm:"embedded;embeddedPrefix:minTemp_"`
	AvailableForPickUp                    AvailableForPickUp   `json:"availableForPickUp" gorm:"embedded;embeddedPrefix:availableForPickUp_"`
	MinimumCargoManagementPoints          int                  `json:"minimumCargoManagementPoints"`
	IsNotOfferable                        bool                 `json:"isNotOfferable"`
	BookingContactPhoneNumber             string               `json:"bookingContactPhoneNumber"`
	IsTwicCardRequired                    bool                 `json:"isTwicCardRequired"`
	IsPersonalProtectiveEquipmentRequired bool                 `json:"isPersonalProtectiveEquipmentRequired"`
	HasOverDimensionalItems               bool                 `json:"hasOverDimensionalItems"`
	IsHeavyHaul                           bool                 `json:"isHeavyHaul"`
	LoadJoin                              string               `json:"load_join"`
	CreatedDateTime                       string               `json:"createdDateTime"`
	UpdatedDateTime                       string               `json:"updatedDateTime"`
	AssignedRep                           string               `json:"assignedRep"`
	CalculatedPickUpByDateTime            string               `json:"calculatedPickUpByDateTime"`
	CalculatedDeliverByDateTime           string               `json:"calculatedDeliverByDateTime"`
	AvailableLoadCosts                    []AvailableLoadCost  `json:"availableLoadCosts" gorm:"-"`
	Stops                                 []Stop               `json:"stops" gorm:"-"`
	Modes                                 []string             `json:"modes" gorm:"-"`
}

type Distance struct {
	Miles      float64 `json:"miles"`
	Kilometers float64 `json:"kilometers"`
}

type Weight struct {
	Pounds    float64 `json:"pounds"`
	Kilograms float64 `json:"kilograms"`
}

type Equipment struct {
	Length Dimension `json:"length" gorm:"embedded;embeddedPrefix:length_"`
	Width  Dimension `json:"width" gorm:"embedded;embeddedPrefix:width_"`
	Height Dimension `json:"height" gorm:"embedded;embeddedPrefix:height_"`
}

type SpecializedEquipment struct {
	Code        string    `json:"code"`
	Description string    `json:"description"`
	Length      Dimension `json:"length" gorm:"embedded;embeddedPrefix:length_"`
	Width       Dimension `json:"width" gorm:"embedded;embeddedPrefix:width_"`
	Height      Dimension `json:"height" gorm:"embedded;embeddedPrefix:height_"`
}

type Dimension struct {
	Standard float64 `json:"standard"`
	Metric   float64 `json:"metric"`
}

type Stop struct {
	StopNumber                      int          `json:"stopNumber"`
	SequenceNumber                  int          `json:"sequenceNumber"`
	StopType                        string       `json:"stopType"`
	CalculatedArriveByStartDateTime string       `json:"calculatedArriveByStartDateTime"`
	CalculatedArriveByEndDateTime   string       `json:"calculatedArriveByEndDateTime"`
	HeavyWeight                     int          `json:"heavyWeight"`
	MaxWeight                       WeightDetail `json:"maxWeight"`
	WarehouseInformation            Location     `json:"warehouseInformation"`
	IsScheduledOpenTimeSpecified    bool         `json:"isScheduledOpenTimeSpecified"`
	IsScheduledCloseTimeSpecified   bool         `json:"isScheduledCloseTimeSpecified"`
}

type WeightDetail struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

type AvailableLoadCost struct {
	LoadNumber        int     `json:"loadNumber"`
	CarrierCode       string  `json:"carrierCode"`
	ExpirationDate    string  `json:"expirationDate"`
	Type              string  `json:"type"`
	Code              string  `json:"code"`
	Description       string  `json:"description"`
	SourceCostPerUnit float64 `json:"sourceCostPerUnit"`
	Units             int     `json:"units"`
	CurrencyCode      string  `json:"currencyCode"`
	EmployeeCode      string  `json:"employeeCode"`
	EmployeeBranch    string  `json:"employeeBranch"`
	Score             int     `json:"score"`
	CreatedDateTime   string  `json:"createdDateTime"`
	UpdatedDateTime   string  `json:"updatedDateTime"`
	BinCostKey        string  `json:"binCostKey"`
	BinOfferable      bool    `json:"binOfferable"`
}

type Temperature struct {
	Metric      float64 `json:"metric"`
	MetricUom   string  `json:"metricUom"`
	Standard    int     `json:"standard"`
	StandardUom string  `json:"standardUom"`
}

type AvailableForPickUp struct {
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	Minimum   string `json:"minimum"`
	Maximum   string `json:"maximum"`
}

// Milestone-related models

// ShipmentIdentifier contains different common reference numbers that can be used to identify the shipment.
type ShipmentIdentifier struct {
	ShipmentNumber  string `json:"shipmentNumber,omitempty"`
	ShipperBillToId string `json:"shipperBillToId,omitempty"`
	ProNumber       string `json:"proNumber,omitempty"`
	TrackingNumber  string `json:"trackingNumber,omitempty"`
	ContainerNumber string `json:"containerNumber,omitempty"`
}

// DateTime contains the different types of dates that can apply to a milestone.
type DateTime struct {
	EventDateTime          string            `json:"eventDateTime,omitempty"`
	EstimatedTimeOfArrival string            `json:"estimatedTimeOfArrival,omitempty"`
	AppointmentWindow      AppointmentWindow `json:"appointmentWindow,omitempty"`
}

// AppointmentWindow represents the open and close date and time for an appointment.
type AppointmentWindow struct {
	OpenDateTime  string `json:"openDateTime,omitempty"`
	CloseDateTime string `json:"closeDateTime,omitempty"`
}

// Carrier contains information associated with the carrier for the milestone.
type Carrier struct {
	Name                     string        `json:"name,omitempty"`
	DotNumber                string        `json:"dotNumber,omitempty"`
	McNumber                 string        `json:"mcNumber,omitempty"`
	Scac                     string        `json:"scac,omitempty"`
	Temperature              float64       `json:"temperature,omitempty"`
	TemperatureUnitOfMeasure string        `json:"temperatureUnitOfMeasure,omitempty"`
	VehicleDetail            VehicleDetail `json:"vehicleDetail,omitempty"`
}

// VehicleDetail contains details about the vehicle used for the shipment.
type VehicleDetail struct {
	TractorNumber            string                   `json:"tractorNumber,omitempty"`
	TrailerNumber            string                   `json:"trailerNumber,omitempty"`
	LicensePlate             string                   `json:"licensePlate,omitempty"`
	LicensePlateState        string                   `json:"licensePlateState,omitempty"`
	Vin                      string                   `json:"vin,omitempty"`
	DriverContactInformation DriverContactInformation `json:"contactInformation,omitempty"`
}

// ContactInformation contains contact details for the driver and dispatch.
type DriverContactInformation struct {
	DriverName        string `json:"driverName,omitempty"`
	DriverPhone       string `json:"driverPhone,omitempty"`
	SecondDriverName  string `json:"secondDriverName,omitempty"`
	SecondDriverPhone string `json:"secondDriverPhone,omitempty"`
	DispatchName      string `json:"dispatchName,omitempty"`
	DispatchPhone     string `json:"dispatchPhone,omitempty"`
}

// Location contains information associated with the location for the milestone.
type MilestoneLocation struct {
	Type               string  `json:"type,omitempty"`
	StopSequenceNumber int     `json:"stopSequenceNumber,omitempty"`
	SequenceNumber     int     `json:"sequenceNumber,omitempty"`
	Name               string  `json:"name,omitempty"`
	LocationId         string  `json:"locationId,omitempty"`
	Address            Address `json:"address,omitempty"`
}

// Address represents a physical address.
type Address struct {
	Address1          string  `json:"address1,omitempty"`
	Address2          string  `json:"address2,omitempty"`
	City              string  `json:"city,omitempty"`
	Latitude          float64 `json:"latitude,omitempty"`
	Longitude         float64 `json:"longitude,omitempty"`
	StateProvinceCode string  `json:"stateProvinceCode,omitempty"`
	PostalCode        string  `json:"postalCode,omitempty"`
	Country           string  `json:"country"`
}

// Item contains details about an item tied to the milestone.
type Item struct {
	ItemId              string  `json:"itemId,omitempty"`
	Weight              float64 `json:"weight,omitempty"`
	WeightUnitOfMeasure string  `json:"weightUnitOfMeasure,omitempty"`
	Quantity            int     `json:"quantity,omitempty"`
	Pallets             int     `json:"pallets,omitempty"`
}

// SupplementalInformation allows updating entity to provide additional details pertaining to the milestone.
type SupplementalInformation struct {
	ReasonCode string `json:"reasonCode,omitempty"`
	Notes      string `json:"notes,omitempty"`
}

// MilestoneUpdate represents the JSON object for a milestone update.
type MilestoneUpdate struct {
	EventCode               string                   `json:"eventCode"`                         // Required
	ShipmentIdentifier      ShipmentIdentifier       `json:"shipmentIdentifier"`                // Required
	DateTime                DateTime                 `json:"dateTime"`                          // Required
	Carrier                 *Carrier                 `json:"carrier,omitempty"`                 // Optional
	Location                MilestoneLocation        `json:"location"`                          // Required
	Items                   []Item                   `json:"items,omitempty"`                   // Optional
	SupplementalInformation *SupplementalInformation `json:"supplementalInformation,omitempty"` // Optional
}

// Our Platform related models

// Driver data we receive from our platform
type DriverData struct {
	DriverID string `json:"driverId"`
	Location string `json:"location"`
	Status   string `json:"status"`
	// Add more fields as needed
}

// QuoteRequest represents a single quote from C.H. Robinson.
type QuoteRequest struct {
	QuoteID          string            `json:"quoteId"`          // Unique identifier for the quote
	Customer         Customer          `json:"customer"`         // Customer information for the quote
	BillTo           BillTo            `json:"billTo"`           // Billing information for the quote
	Service          Service           `json:"service"`          // Service details for the quote
	Origin           Location          `json:"origin"`           // Origin location for the shipment
	Destination      Location          `json:"destination"`      // Destination location for the shipment
	OnlineParties    []OnlineParty     `json:"onlineParties"`    // Online parties associated with the quote
	Notes            string            `json:"notes"`            // Additional notes for the quote
	ReferenceNumbers []ReferenceNumber `json:"referenceNumbers"` // Reference numbers for the quote
}

// ReferenceNumber represents a reference number associated with a quote.
type ReferenceNumber struct {
	Type  string `json:"type"`  // Type of the reference number
	Value string `json:"value"` // Value of the reference number
}

type Contact struct {
	Name           string          `json:"name"`
	Type           string          `json:"type"`
	CompanyName    string          `json:"companyName"`
	ContactMethods []ContactMethod `json:"contactMethods" gorm:"-"`
}

// ContactMethod represents a method of contacting an entity.
type ContactMethod struct {
	Method string `json:"method"` // Method of contact (e.g., Phone, Email)
	Value  string `json:"value"`  // Contact information (e.g., phone number, email address)
}

// Customer represents customer information for a quote.
type Customer struct {
	CustomerCode     string            `json:"customerCode"`     // Unique code for the customer
	Contacts         []Contact         `json:"contacts"`         // Contact information for the customer
	ReferenceNumbers []ReferenceNumber `json:"referenceNumbers"` // Reference numbers for the customer
}

// BillTo represents billing information for a quote.
type BillTo struct {
	Contacts         []Contact         `json:"contacts"`         // Contact information for billing
	ReferenceNumbers []ReferenceNumber `json:"referenceNumbers"` // Reference numbers for billing
}

// Service represents service details for a quote.
type Service struct {
	ReferenceNumbers          []ReferenceNumber `json:"referenceNumbers"`          // Reference numbers for the service
	SuggestedScac             string            `json:"suggestedScac"`             // Suggested SCAC code for the carrier
	SuggestedCarrierPartyCode string            `json:"suggestedCarrierPartyCode"` // Suggested carrier party code
}

// OnlineParty represents an online party associated with a quote.
type OnlineParty struct {
	PartyType              string `json:"partyType"`              // Type of the online party
	PartyCode              string `json:"partyCode"`              // Code of the online party
	RelationshipIdentifier string `json:"relationshipIdentifier"` // Identifier for the relationship with the online party
}

// QuoteResponse represents the response from CHRobinson once our GetQuote function executes
type QuoteResponse struct {
	QuoteID        string `json:"quoteId"`
	OrderNumber    int    `json:"orderNumber"`
	TrackingNumber string `json:"trackingNumber"`
}

// Rating Request structs
type SpecialRequirementRating struct {
	LiftGate                 bool `json:"liftGate"`
	InsidePickup             bool `json:"insidePickup"`
	ResidentialNonCommercial bool `json:"residentialNonCommercial"`
	LimitedAccess            bool `json:"limitedAccess"`
	TradeShoworConvention    bool `json:"tradeShoworConvention"`
	ConstructionSite         bool `json:"constructionSite"`
	DropOffAtCarrierTerminal bool `json:"dropOffAtCarrierTerminal"`
	DropTrailer              bool `json:"dropTrailer"`
	InsideDelivery           bool `json:"insideDelivery,omitempty"`
	PickupAtCarrierTerminal  bool `json:"pickupAtCarrierTerminal,omitempty"`
}

type RatingLocation struct {
	Name               string                   `json:"name"`
	Address1           string                   `json:"address1"`
	Address2           string                   `json:"address2,omitempty"`
	Address3           string                   `json:"address3,omitempty"`
	City               string                   `json:"city"`
	StateProvinceCode  string                   `json:"stateProvinceCode"`
	CountryCode        string                   `json:"countryCode"`
	PostalCode         string                   `json:"postalCode"`
	OpenDateTime       string                   `json:"openDateTime"`
	CloseDateTime      string                   `json:"closeDateTime"`
	Latitude           float64                  `json:"latitude,omitempty"`
	Longitude          float64                  `json:"longitude,omitempty"`
	SpecialRequirement SpecialRequirementRating `json:"specialRequirement"`
	IsPort             bool                     `json:"isPort"`
	UnLocode           string                   `json:"unLocode,omitempty"`
	Iata               string                   `json:"iata,omitempty"`
	CustomerLocationId string                   `json:"customerLocationId"`
	SequenceNumber     int                      `json:"sequenceNumber"`
	ReferenceNumbers   []ReferenceNumber        `json:"referenceNumbers"`
}

type RatingItem struct {
	Description                  string            `json:"description"`
	FreightClass                 float64           `json:"freightClass"`
	Weight                       float64           `json:"weight"`
	WeightUnitOfMeasure          string            `json:"weightUnitOfMeasure"`
	PackagingLength              float64           `json:"packagingLength"`
	PackagingWidth               float64           `json:"packagingWidth"`
	PackagingHeight              float64           `json:"packagingHeight"`
	PackagingUnitOfMeasure       string            `json:"packagingUnitOfMeasure"`
	Pallets                      float64           `json:"pallets"`
	Quantity                     float64           `json:"quantity"`
	PalletSpaces                 float64           `json:"palletSpaces"`
	PackagingVolume              float64           `json:"packagingVolume"`
	PackagingVolumeUnitOfMeasure string            `json:"packagingVolumeUnitOfMeasure"`
	Density                      float64           `json:"density"`
	LinearSpace                  float64           `json:"linearSpace"`
	InsuranceValue               float64           `json:"insuranceValue"`
	PackagingType                string            `json:"packagingType"`
	ProductCode                  string            `json:"productCode"`
	TemperatureType              string            `json:"temperatureType"`
	TemperatureUnit              string            `json:"temperatureUnit"`
	RequiredTemperatureHigh      float64           `json:"requiredTemperatureHigh"`
	RequiredTemperatureLow       float64           `json:"requiredTemperatureLow"`
	UnitsPerPallet               float64           `json:"unitsPerPallet"`
	UnitWeight                   float64           `json:"unitWeight"`
	UnitVolume                   float64           `json:"unitVolume"`
	IsStackable                  bool              `json:"isStackable"`
	IsOverWeightOverDimensional  bool              `json:"isOverWeightOverDimensional"`
	IsUsedGood                   bool              `json:"isUsedGood"`
	IsHazardous                  bool              `json:"isHazardous"`
	HazardousDescription         string            `json:"hazardousDescription"`
	HazardousEmergencyPhone      string            `json:"hazardousEmergencyPhone"`
	NationalMotorFreightClass    string            `json:"nationalMotorFreightClass"`
	UpcNumber                    string            `json:"upcNumber"`
	SkuNumber                    string            `json:"skuNumber"`
	PluNumber                    string            `json:"pluNumber"`
	PickupSequenceNumber         int               `json:"pickupSequenceNumber"`
	DropSequenceNumber           int               `json:"dropSequenceNumber"`
	ReferenceNumbers             []ReferenceNumber `json:"referenceNumbers"`
	UnNumber                     string            `json:"unNumber"`
	PackagingGroup               string
	HazmatClassCode              string `json:"hazmatClassCode"`
}

type TransportMode struct {
	Mode       string `json:"mode"`
	Equipments []struct {
		EquipmentType string `json:"equipmentType"`
		Quantity      int    `json:"quantity"`
	} `json:"equipments"`
}

// RatingRequest represents a request for rating.
type RatingRequest struct {
	Items                []RatingItem      `json:"items"`
	Origin               RatingLocation    `json:"origin"`
	Destination          RatingLocation    `json:"destination"`
	AdditionalLocations  []RatingLocation  `json:"additionalLocations,omitempty"`
	ShipDate             string            `json:"shipDate"`
	Platform             string            `json:"platform,omitempty"`
	TransactionId        string            `json:"transactionId,omitempty"`
	CustomerCode         string            `json:"customerCode"`
	TransportModes       []TransportMode   `json:"transportModes"`
	ReferenceNumbers     []ReferenceNumber `json:"referenceNumbers,omitempty"`
	OptionalAccessorials []string          `json:"optionalAccessorials,omitempty"`
}

//Rating Response from CHRobinson

type RatingResponse struct {
	QuoteSummaries []QuoteSummary `json:"quoteSummaries"`
}

type QuoteSummary struct {
	QuoteID                int64    `json:"quoteId"`
	Customer               Customer `json:"customer"` // Define the Customer struct based on the expected fields
	TotalCharge            float64  `json:"totalCharge"`
	TotalFreightCharge     float64  `json:"totalFreightCharge"`
	TotalAccessorialCharge float64  `json:"totalAccessorialCharge"`
	Transit                Transit  `json:"transit"`
	Rates                  []Rate   `json:"rates"`
	TransportModeType      string   `json:"transportModeType"`
	EquipmentType          string   `json:"equipmentType"`
	QuoteSource            string   `json:"quoteSource"`
}

type Transit struct {
	MinimumTransitDays int `json:"minimumTransitDays"`
	MaximumTransitDays int `json:"maximumTransitDays"`
}

type Rate struct {
	RateID        int64   `json:"rateId"`
	TotalRate     float64 `json:"totalRate"`
	UnitRate      float64 `json:"unitRate"`
	Quantity      float64 `json:"quantity"`
	RateCode      string  `json:"rateCode"`
	RateCodeValue string  `json:"rateCodeValue"`
	CurrencyCode  string  `json:"currencyCode"`
	IsOptional    bool    `json:"isOptional"`
}

//Estimating Freight Class Structs

type FreightClassEstimateRequest struct {
	Length                 float64 `json:"length"`
	Width                  float64 `json:"width"`
	Height                 float64 `json:"height"`
	LinearUnit             string  `json:"linearUnit"`
	Weight                 float64 `json:"weight"`
	WeightUnit             string  `json:"weightUnit"`
	OriginCountryCode      string  `json:"originCountryCode"`
	DestinationCountryCode string  `json:"destinationCountryCode"`
	CustomerCode           string  `json:"customerCode"`
	Quantity               float64 `json:"quantity"`
	ShipmentDate           string  `json:"shipmentDate,omitempty"`
	TotalWeight            float64 `json:"totalWeight,omitempty"`
}

type FreightClassEstimateResponse struct {
	EstimatedFreightClassByDensityOnly int `json:"EstimatedFreightClassByDensityOnly"`
}

//For testing token

type TestTokenResponse struct {
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	AccessToken string `json:"access_token"`
	Scope       string `json:"scope"`
}

//

type LoadBookingRequest struct {
	LoadNumber         int              `json:"loadNumber"`
	CarrierCode        string           `json:"carrierCode"`
	EmptyDateTime      string           `json:"emptyDateTime"`
	EmptyLocation      Location         `json:"emptyLocation"`
	AvailableLoadCosts []LoadCost       `json:"availableLoadCosts"`
	RateConfirmation   RateConfirmation `json:"rateConfirmation"`
}

type RateConfirmation struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

// type LoadCost struct {
// 	ExpirationDate    string  `json:"expirationDate"`
// 	Type              string  `json:"type"`
// 	Code              string  `json:"code"`
// 	Description       string  `json:"description"`
// 	SourceCostPerUnit float64 `json:"sourceCostPerUnit"`
// 	Units             int     `json:"units"`
// 	CurrencyCode      string  `json:"currencyCode"`
// 	EmployeeCode      string  `json:"employeeCode"`
// 	EmployeeBranch    string  `json:"employeeBranch"`
// 	Score             int     `json:"score"`
// 	CreatedDateTime   string  `json:"createdDateTime"`
// 	UpdatedDateTime   string  `json:"updatedDateTime"`
// 	BinCostKey        string  `json:"binCostKey"`
// 	BinOfferable      bool    `json:"binOfferable"`
// }

type LoadCost struct {
	Type              string  `json:"type"`
	Code              string  `json:"code"`
	Description       string  `json:"description"`
	SourceCostPerUnit float64 `json:"sourceCostPerUnit"`
	Units             int     `json:"units"`
	CurrencyCode      string  `json:"currencyCode"`
}

type LoadOfferRequest struct {
	CarrierCode  string `json:"carrierCode"`
	OfferPrice   int    `json:"offerPrice"`
	OfferNote    string `json:"offerNote,omitempty"`
	CurrencyCode string `json:"currencyCode,omitempty"`
	// AvailableLoadCost int    `json:"availableLoadCost"`
}

// OfferResponse represents the structure for an offer response callback.
type OfferResponse struct {
	ID               uint     `gorm:"primaryKey" json:"id"`
	LoadNumber       int      `json:"loadNumber"`
	CarrierCode      string   `json:"carrierCode"`
	OfferRequestId   string   `json:"offerRequestId"`
	OfferId          int      `json:"offerId"`
	OfferResult      string   `json:"offerResult"`
	Price            int      `json:"price"`
	CurrencyCode     string   `json:"currencyCode"`
	RejectReasons    []string `gorm:"-" json:"rejectReasons"`         // Ignore by GORM, handled manually
	RejectReasonsStr string   `json:"-" gorm:"column:reject_reasons"` // JSON string representation
	Status           string   `json:"status"`
	CreatedAt        string   `json:"created_at"`
	UpdatedAt        string   `json:"updated_at"`
}

type Truck struct {
	Id                    int64   `gorm:"column:id"`
	Width                 float64 `gorm:"column:width"`
	Length                float64 `gorm:"column:length"`
	Height                float64 `gorm:"column:height"`
	Vin                   string  `gorm:"column:vin"`
	Year                  int     `gorm:"column:year"`
	PlateNumber           string  `gorm:"column:plate_number"`
	TruckTypeId           int64   `gorm:"column:truck_type"`
	CargoAreaTypeId       int64   `gorm:"column:cargo_area_type_id"`
	CreatedAt             string  `gorm:"column:created_at"`
	UpdatedAt             string  `gorm:"column:updated_at"`
	CompanyId             int64   `gorm:"column:company_id"`
	Status                int     `gorm:"column:status"`
	RejectMessage         string  `gorm:"column:reject_message"`
	Radius                int     `gorm:"column:radius"`
	NotAvailableUntil     string  `gorm:"column:not_available_until"`
	Brand                 string  `gorm:"column:brand"`
	Model                 string  `gorm:"column:model"`
	Identifier            string  `gorm:"column:identifier"`
	Weight                int     `gorm:"column:weight"`
	ExternalHoldUntil     string  `gorm:"column:external_hold_until"`
	Active                int8    `gorm:"column:active"`
	LiftGate              int8    `gorm:"column:lift_gate"`
	DistanceMin           int     `gorm:"column:distance_min"`
	DistanceMax           int     `gorm:"column:distance_max"`
	TelegramHoldUntil     string  `gorm:"column:telegram_hold_until"`
	PreferredTypeId       int     `gorm:"column:preferred_type_id"`
	IsUsCitizen           bool    `gorm:"column:is_us_citizen"`
	AutoBid               bool    `gorm:"column:auto_bid"`
	SuggestedPriceEnabled bool    `gorm:"column:suggested_price_enabled"`
	DoorHeight            float64 `gorm:"column:door_height"`
	DoorWidth             float64 `gorm:"column:door_width"`
	PalletJack            bool    `gorm:"column:pallet_jack"`
	PricingPercentage     int     `gorm:"column:pricing_percentage"`
	PricingFixed          int     `gorm:"column:pricing_fixed"`
	IsTeam                bool    `gorm:"column:is_team"`
	SkipVanCheck          bool    `gorm:"column:skip_van_check"`
	IsRefrigerated        bool    `gorm:"column:is_refrigerated"`
	BorderAccess          bool    `gorm:"column:border_access"`
	AppState              string  `gorm:"column:app_state"`
}

// TruckAdditionalData represents additional data related to a truck.
type TruckAdditionalData struct {
	UserName       string
	DriverName     string
	TelegramLink   string
	PhoneNumber    string
	TruckType      string
	DispatcherName string
}

type LoaderLocation struct {
	ZipCode       string `json:"zip_code"`
	Latitude      string `json:"latitude"`
	Longitude     string `json:"longitude"`
	IntegrationID string `json:"integration_id"`
	Address       string `json:"address"`
}

type LoaderLocationsResponse struct {
	Data []LoaderLocation `json:"data"`
}

type CombinedShipmentInfo struct {
	ShipmentInfo
	TruckData                 Truck
	LocationData              PseudoLocations
	AdditionalData            TruckAdditionalData
	BookingContactPhoneNumber string `json:"bookingContactPhoneNumber"`
}

type PseudoLocations struct {
	Id        int64   `json:"id"`
	Zip       string  `json:"zip"`
	Address   string  `json:"address"`
	From      string  `json:"from"`
	To        string  `json:"to"`
	TruckId   int64   `json:"truck_id"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
}
type Order struct {
	ID                int64     `gorm:"primary_key;auto_increment" json:"id"`
	OrderNumber       string    `gorm:"type:varchar(255)" json:"order_number"`
	PickupLocation    string    `gorm:"type:varchar(255)" json:"pickup_location"`
	DeliveryLocation  string    `gorm:"type:varchar(255)" json:"delivery_location"`
	PickupDate        time.Time `gorm:"type:datetime" json:"pickup_date"`
	LocalPickupDate   time.Time `gorm:"type:datetime" json:"local_pickup_date"`
	PickupAsap        bool      `gorm:"type:tinyint(1)" json:"pickup_asap"`
	DeliveryDate      time.Time `gorm:"type:datetime" json:"delivery_date"`
	DockLevel         bool      `gorm:"type:tinyint(1)" json:"dock_level"`
	DeliveryZip       string    `gorm:"type:varchar(255)" json:"delivery_zip"`
	Pays              int       `json:"pays"`
	PaysRate          float64   `gorm:"type:double(8,2)" json:"pays_rate"`
	TruckTypeID       int64     `json:"truck_type_id"`
	Link              string    `gorm:"type:varchar(255)" json:"link"`
	OrderTypeID       int64     `json:"order_type_id"`
	ExternalLink      string    `gorm:"type:varchar(255)" json:"external_link"`
	LiftGate          bool      `gorm:"type:tinyint(1)" json:"lift_gate"`
	OriginalTruckSize string    `gorm:"type:varchar(255)" json:"original_truck_size"`
	Extra             string    `gorm:"type:varchar(255)" json:"extra"`
	Shipper           string    `gorm:"type:varchar(545)" json:"shipper"`
	Receiver          string    `gorm:"type:varchar(545)" json:"receiver"`
	SelectedTrucks    int       `json:"selected_trucks"`
	SentTrucks        int       `json:"sent_trucks"`
}

type ShipmentDetails struct {
	Time        string `json:"time"`
	CarrierCode string `json:"carrierCode"`
	Scac        string `json:"scac"`
	LoadNumber  int    `json:"loadNumber"`
	ClientId    string `json:"clientId"`
	EventTime   string `json:"eventTime"`
	Event       Event  `json:"event"`
}

type Flags struct {
	IsTeamOperated    bool `json:"isTeamOperated"`
	IsHazMat          bool `json:"isHazMat"`
	IsRegulatedByStf  bool `json:"isRegulatedByStf"`
	IsTankerEndorsed  bool `json:"isTankerEndorsed"`
	HasOpenClaims     bool `json:"hasOpenClaims"`
	HasOpenEvents     bool `json:"hasOpenEvents"`
	IsControl         bool `json:"isControl"`
	IsForwardingHouse bool `json:"isForwardingHouse"`
}

type ItemContainer struct {
	ContainerDetails  ContainerDetails `json:"containerDetails"`
	ItemContainerId   int              `json:"itemContainerId"`
	ItemContainerType int              `json:"itemContainerType"`
	PackageDetails    PackageDetails   `json:"packageDetails"`
	ReferenceNumbers  ReferenceNumbers `json:"referenceNumbers"`
	StopActivityIds   []string         `json:"stopActivityIds"`
}

type ContainerDetails struct {
	ContainerCondition                 string `json:"containerCondition"`
	ContainerEmptyReturnDate           string `json:"containerEmptyReturnDate"`
	ContainerLastFreeDayForEmptyReturn string `json:"containerLastFreeDayForEmptyReturn"`
	ContainerNumber                    string `json:"containerNumber"`
	ContainerStatus                    int    `json:"containerStatus"`
	ContainerType                      string `json:"containerType"`
	IsHot                              bool   `json:"isHot"`
	VgmAcceptedDate                    string `json:"vgmAcceptedDate"`
}

type PackageDetails struct {
	PackageType    string `json:"packageType"`
	SignedBy       string `json:"signedBy"`
	TrackingNumber int    `json:"trackingNumber"`
}

type ReferenceNumbers struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type ShipmentEquipment struct {
	Description      string           `json:"description"`
	Height           Dimension        `json:"height"`
	Length           Dimension        `json:"length"`
	Width            Dimension        `json:"width"`
	Temperature      Dimension        `json:"temperature"`
	ReferenceNumbers ReferenceNumbers `json:"referenceNumbers"`
}

type HazMatDetails struct {
	Class              string `json:"class"`
	EmergencyPhone     int    `json:"emergencyPhone"`
	UnNumberWithPrefix string `json:"unNumberWithPrefix"`
	UnNumber           int    `json:"unNumber"`
}

type ItemEquipment struct {
	CoilRacksCount              int    `json:"coilRacksCount"`
	FlatBedEquipmentCode        string `json:"flatBedEquipmentCode"`
	FlatBedEquipmentDescription string `json:"flatBedEquipmentDescription"`
	LoadingTypeDescription      string `json:"loadingTypeDescription"`
	MaxTrailerAge               int    `json:"maxTrailerAge"`
	RequiredChainsCount         int    `json:"requiredChainsCount"`
	RequiredLocksCount          int    `json:"requiredLocksCount"`
	RequiredSquaresCount        int    `json:"requiredSquaresCount"`
	RequiredStrapsCount         int    `json:"requiredStrapsCount"`
	RequiredTimbersCount        int    `json:"requiredTimbersCount"`
	TarpDescription             string `json:"tarpDescription"`
}

type Book struct {
	BookedDateTime           string           `json:"bookedDateTime"`
	BookId                   int              `json:"bookId"`
	Distance                 int              `json:"distance"`
	ExpectedPickupDateTime   string           `json:"expectedPickupDateTime"`
	ExpectedDeliveryDateTime string           `json:"expectedDeliveryDateTime"`
	ReferenceNumbers         ReferenceNumbers `json:"referenceNumbers"`
}

type Activity struct {
	Appointment        Appointment `json:"appointment"`
	BookIds            []int       `json:"bookIds"`
	IsDropTrailer      bool        `json:"isDropTrailer"`
	IsRequestedTimeSet bool        `json:"isRequestedTimeSet"`
	ItemContainerId    string      `json:"itemContainerId"`
	ItemIds            []string    `json:"itemIds"`
	RequestedDateTime  string      `json:"requestedDateTime"`
	StopActivityId     string      `json:"stopActivityId"`
	StopId             int         `json:"stopId"`
}

type Appointment struct {
	CloseDateTime         string `json:"closeDateTime"`
	IsClostTimeSet        bool   `json:"isClostTimeSet"`
	IsOpenTimeSet         bool   `json:"isOpenTimeSet"`
	OpenDateTime          string `json:"openDateTime"`
	SchedulingOptionsCode string `json:"schedulingOptionsCode"`
}

type StopLocation struct {
	Address1                        string   `json:"address1"`
	Address2                        string   `json:"address2"`
	Location                        Location `json:"location"`
	WarehouseCloseTime              string   `json:"warehouseCloseTime"`
	WarehouseCode                   string   `json:"warehouseCode"`
	WarehouseCustomerLocationNumber string   `json:"warehouseCustomerLocationNumber"`
	WarehouseName                   string   `json:"warehouseName"`
	WarehouseOpenTime               string   `json:"warehouseOpenTime"`
	WarehousePhoneNumber            string   `json:"warehousePhoneNumber"`
}
