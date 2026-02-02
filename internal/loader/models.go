package loader

// LoaderOrder is the payload sent to the Loader API orders endpoint.
type LoaderOrder struct {
	Source              string  `json:"source"`
	OrderNumber         string  `json:"orderNumber"`
	PickupLocation      string  `json:"pickupLocation"`
	DeliveryLocation    string  `json:"deliveryLocation"`
	PickupDate          string  `json:"pickupDate"`
	DeliveryDate        string  `json:"deliveryDate"`
	PickupTime          string  `json:"pickupTime"`
	DeliveryTime        string  `json:"deliveryTime"`
	SuggestedTruckSize  string  `json:"suggestedTruckSize"`
	TruckTypeId         int     `json:"truckTypeId"`
	OriginalTruckSize   string  `json:"originalTruckSize"`
	PickupZip           string  `json:"pickupZip"`
	DeliveryZip         string  `json:"deliveryZip"`
	PickupCity          string  `json:"pickupCity"`
	PickupState         string  `json:"pickupState"`
	PickupCountry       string  `json:"pickupCountry"`
	PickupCountryCode   string  `json:"pickupCountryCode"`
	PickupCountryName   string  `json:"pickupCountryName"`
	DeliveryCity        string  `json:"deliveryCity"`
	DeliveryState       string  `json:"deliveryState"`
	DeliveryCountry     string  `json:"deliveryCountry"`
	DeliveryCountryCode string  `json:"deliveryCountryCode"`
	DeliveryCountryName string  `json:"deliveryCountryName"`
	EstimatedMiles      float64 `json:"estimatedMiles"`
	OrderTypeId         int     `json:"orderTypeId"`
	Length              float64 `json:"length"`
	Width               float64 `json:"width"`
	Height              float64 `json:"height"`
	Weight              float64 `json:"weight"`
	CarrierPay          float64 `json:"carrierPay"`
	CarrierPayRate      float64 `json:"carrierPayRate"`
	Bond                int     `json:"bond"`
	BondTypeID          int     `json:"bondTypeId"`
	TruckCompanyEmail   string  `json:"truckCompanyEmail"`
	SpecInfo            string  `json:"specInfo"`
	PointOfContactPhone string  `json:"pointOfContactPhone"`
	LoadTruckstopXML    string  `json:"loadTruckstopXML"`
	AirRide             int     `json:"airRide"`
	LiftGate            int     `json:"liftGate"`

	LoadType         string `json:"loadType"`
	Quantity         int    `json:"quantity"`
	Stops            int    `json:"stops"`
	TruckCompanyName string `json:"truckCompanyName"`
}
