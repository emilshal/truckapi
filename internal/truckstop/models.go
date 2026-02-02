package truckstop

import (
	"encoding/xml"
	"strconv"
	"strings"
	"truckapi/internal/chrobinson"
)

// Envelope for GetMultipleLoadDetailResults
type MultipleLoadDetailEnvelope struct {
	XMLName xml.Name `xml:"Envelope"`
	Body    struct {
		Response struct {
			Result struct {
				DetailResults struct {
					Loads []LoadDetail `xml:"MultipleLoadDetailResult"`
				} `xml:"DetailResults"`
			} `xml:"GetMultipleLoadDetailResultsResult"`
		} `xml:"GetMultipleLoadDetailResultsResponse"`
	} `xml:"Body"`
}

type LoadDetail struct {
	XMLName xml.Name `xml:"MultipleLoadDetailResult"`

	ID string `xml:"ID"`

	OriginCity  string `xml:"OriginCity"`
	OriginState string `xml:"OriginState"`
	OriginZip   string `xml:"OriginZip"`

	DestinationCity  string `xml:"DestinationCity"`
	DestinationState string `xml:"DestinationState"`
	DestinationZip   string `xml:"DestinationZip"`

	Equipment string `xml:"Equipment"`

	PaymentAmount NullFloat64 `xml:"PaymentAmount"`
	Mileage       NullInt     `xml:"Mileage"`
	Weight        NullInt     `xml:"Weight"`
	Length        NullFloat64 `xml:"Length"`
	Width         NullFloat64 `xml:"Width"`
	Dims          string      `xml:"Dims"`
	SpecInfo      string      `xml:"SpecInfo"`

	PickUpDate   string `xml:"PickupDate"`
	PickUpTime   string `xml:"PickupTime"`
	DeliveryTime string `xml:"DeliveryTime"`

	TruckCompanyName    string  `xml:"TruckCompanyName"`
	TruckCompanyEmail   string  `xml:"TruckCompanyEmail"`
	PointOfContactPhone string  `xml:"PointOfContactPhone"`
	Bond                NullInt `xml:"Bond"`
	BondTypeID          NullInt `xml:"BondTypeID"`
	LoadType            string  `xml:"LoadType"`
	Quantity            NullInt `xml:"Quantity"`
	Stops               NullInt `xml:"Stops"`
}

type TruckTypeMapping struct {
	TruckTypeId        int
	SuggestedTruckSize string
}

var TruckstopEquipmentMapping = map[string]TruckTypeMapping{
	// HS → SMALL STRAIGHT (TruckTypeId = 1)
	"HS":  {1, "SMALL STRAIGHT"},
	"HSO": {1, "SMALL STRAIGHT"},
	"HSD": {1, "SMALL STRAIGHT"},
	"HOT": {1, "SMALL STRAIGHT"},

	// Sprinter → TruckTypeId = 3
	"SPV":  {3, "SPRINTER"},
	"VCAR": {3, "SPRINTER"},
	"SPVR": {3, "SPRINTER"},
	"CVR":  {3, "SPRINTER"},
	"CSV":  {3, "SPRINTER"},

	// Van (SMALL STRAIGHT) → TruckTypeId = 1
	"V":    {1, "SMALL STRAIGHT"},
	"SV":   {1, "SMALL STRAIGHT"},
	"VLG":  {1, "SMALL STRAIGHT"},
	"VM":   {1, "SMALL STRAIGHT"},
	"VA":   {1, "SMALL STRAIGHT"},
	"VB":   {1, "SMALL STRAIGHT"},
	"V-OT": {1, "SMALL STRAIGHT"},
	"VIV":  {1, "SMALL STRAIGHT"},
	"VV":   {1, "SMALL STRAIGHT"},
	"VBV":  {1, "SMALL STRAIGHT"},
	"VBO":  {1, "SMALL STRAIGHT"},
	"VMC":  {1, "SMALL STRAIGHT"},
	"VPU":  {1, "SMALL STRAIGHT"},
	"VUL":  {1, "SMALL STRAIGHT"},

	// Curtain Van (Semi) → SEMI TRUCK (TruckTypeId = 5)
	"CV": {5, "SEMI TRUCK"},

	// Flatbed / Van / Reefer Combo (Semi) → SEMI TRUCK (TruckTypeId = 5)
	"FRV":  {5, "SEMI TRUCK"},
	"VRDD": {5, "SEMI TRUCK"},
	"VRF":  {5, "SEMI TRUCK"},
	"FSDV": {5, "SEMI TRUCK"},
	"FV":   {5, "SEMI TRUCK"},
	"FVVR": {5, "SEMI TRUCK"},
	"RVF":  {5, "SEMI TRUCK"},
	"RFV":  {5, "SEMI TRUCK"},
	"FVR":  {5, "SEMI TRUCK"},
	"VR":   {5, "SEMI TRUCK"},
	"R":    {5, "SEMI TRUCK"},

	// Flatbed / Step Deck (Semi) → SEMI TRUCK (TruckTypeId = 5)
	"SDC":  {5, "SEMI TRUCK"},
	"SDE":  {5, "SEMI TRUCK"},
	"SD":   {5, "SEMI TRUCK"},
	"SDL":  {5, "SEMI TRUCK"},
	"SDO":  {5, "SEMI TRUCK"},
	"F":    {5, "SEMI TRUCK"},
	"FEXT": {5, "SEMI TRUCK"},
	"FINT": {5, "SEMI TRUCK"},
	"FO":   {5, "SEMI TRUCK"},
	"FSD":  {5, "SEMI TRUCK"},
	"FWS":  {5, "SEMI TRUCK"},
	"FA":   {5, "SEMI TRUCK"},
	"A":    {5, "SEMI TRUCK"},
	// Van Flatbed Combo (Semi) → SEMI TRUCK (TruckTypeId = 5)
	"VF": {5, "SEMI TRUCK"},
}

var TruckstopEquipmentNames = map[string]string{
	"HS":   "Hot Shot",
	"SPV":  "Sprinter Van",
	"VCAR": "Sprinter Van",
	"V":    "Van",
	"SV":   "Van",
	"VLG":  "Van with Lift Gate",
	"VM":   "Van",
	"VA":   "Van Air-Ride",
	"VB":   "Van",
	"V-OT": "Van",
	"VIV":  "Van",
	"VV":   "Van",
	"CV":   "Curtain Van",
	"FRV":  "Flatbed/Van/Reefer Combo",
	"VRDD": "Flatbed/Van/Reefer Combo",
	"VRF":  "Flatbed/Van/Reefer Combo",
	"FSDV": "Flatbed/Van/Reefer Combo",
	"FV":   "Flatbed/Van/Reefer Combo",
	"FVVR": "Flatbed/Van/Reefer Combo",
	"RVF":  "Flatbed/Van/Reefer Combo",
	"RFV":  "Flatbed/Van/Reefer Combo",
	"FVR":  "Flatbed/Van/Reefer Combo",
	"VR":   "Flatbed/Van/Reefer Combo",
	"SDC":  "Flatbed / Step Deck",
	"SDE":  "Flatbed / Step Deck",
	"SD":   "Flatbed / Step Deck",
	"SDL":  "Flatbed / Step Deck",
	"SDO":  "Flatbed / Step Deck",
	"F":    "Flatbed / Step Deck",
	"FEXT": "Flatbed / Step Deck",
	"FINT": "Flatbed / Step Deck",
	"FO":   "Flatbed / Step Deck",
	"FSD":  "Flatbed / Step Deck",
	"FWS":  "Flatbed / Step Deck",
	"FA":   "Flatbed Air-Ride",
	"A":    "Flatbed / Step Deck",
	"R":    "Reefer",
	"VBV":  "Box Van with Liftgate",
	"VBO":  "Box Van Other",
	"VMC":  "Medium Cube Van",
	"VPU":  "Pup Van",
	"VUL":  "Utility Van",

	"SPVR": "Sprinter Reefer",
	"CVR":  "Cargo Van Reefer",
	"CSV":  "Compact Sprinter Van",

	"HSO": "Hotshot Other",
	"HSD": "Hotshot with Deck",
	"HOT": "Hotshot Trailer",
	"VF":  "Van Flatbed Combo",
}

// LoadSearchRequest is the <v12:searchRequest> node we send.
type LoadSearchRequest struct {
	XMLName       xml.Name           `xml:"v12:searchRequest"`
	IntegrationId int                `xml:"web:IntegrationId"`
	Password      string             `xml:"web:Password"`
	UserName      string             `xml:"web:UserName"`
	Criteria      LoadSearchCriteria `xml:"web1:Criteria"`
}

// LoadSearchCriteria – **field order MUST match the WSDL**.
type LoadSearchCriteria struct {
	// destination
	DestinationCity    string `xml:"web1:DestinationCity"`
	DestinationCountry string `xml:"web1:DestinationCountry"`
	DestinationRange   int    `xml:"web1:DestinationRange"`
	DestinationState   string `xml:"web1:DestinationState"`

	// filters
	EquipmentType string   `xml:"web1:EquipmentType"`
	HoursOld      int      `xml:"web1:HoursOld"`
	LoadType      LoadType `xml:"web1:LoadType"`

	// origin
	OriginCity      string `xml:"web1:OriginCity"`
	OriginCountry   string `xml:"web1:OriginCountry"`
	OriginLatitude  *int   `xml:"web1:OriginLatitude,omitempty"`
	OriginLongitude *int   `xml:"web1:OriginLongitude,omitempty"`
	OriginRange     int    `xml:"web1:OriginRange"`
	OriginState     string `xml:"web1:OriginState"`

	// paging / sort
	PageNumber     int        `xml:"web1:PageNumber"`
	PageSize       int        `xml:"web1:PageSize"`
	PickupDates    *[]string  `xml:"web1:PickupDates>arr:dateTime,omitempty"`
	SortBy         SortColumn `xml:"web1:SortBy"`
	SortDescending bool       `xml:"web1:SortDescending"`
}

// ────────────────────────────────────────────
// RESPONSE TYPES
// ────────────────────────────────────────────

// Envelope (namespace prefix is ignored by encoding/xml)
type Envelope struct {
	XMLName xml.Name `xml:"Envelope"`
	Body    Body     `xml:"Body"`
}

type Body struct {
	Response Response `xml:"GetLoadSearchResultsResponse"`
}

type Response struct {
	Result LoadSearchResult `xml:"GetLoadSearchResultsResult"`
}

type LoadSearchResult struct {
	Errors        []Error `xml:"Errors>Error"`
	SearchResults []Load  `xml:"SearchResults>LoadSearchItem"`
}

type Error struct {
	ErrorMessage string `xml:"ErrorMessage"`
}

// NullFloat64 swallows <Tag/>, <Tag></Tag> or <Tag>   </Tag> and sets 0.
// Any real number is parsed as usual.
type NullFloat64 float64

func (nf *NullFloat64) UnmarshalXML(d *xml.Decoder, se xml.StartElement) error {
	var txt string
	if err := d.DecodeElement(&txt, &se); err != nil {
		return err
	}
	txt = strings.TrimSpace(txt)
	if txt == "" {
		*nf = 0
		return nil
	}
	v, err := strconv.ParseFloat(txt, 64)
	if err != nil {
		return err
	}
	*nf = NullFloat64(v)
	return nil
}

// Same idea for ints (32-bit is enough for these IDs/distances).
type NullInt int

func (ni *NullInt) UnmarshalXML(d *xml.Decoder, se xml.StartElement) error {
	var txt string
	if err := d.DecodeElement(&txt, &se); err != nil {
		return err
	}
	txt = strings.TrimSpace(txt)
	if txt == "" {
		*ni = 0
		return nil
	}
	v, err := strconv.Atoi(txt)
	if err != nil {
		return err
	}
	*ni = NullInt(v)
	return nil
}

// A single <LoadSearchItem>.  Use string for anything that can appear as
// “—”, “9+”, “$123.45”, etc.; keep numbers numeric where they’re always numeric.
type Load struct {
	XMLName xml.Name `xml:"LoadSearchItem"`

	ID  string `xml:"ID"`
	Age string `xml:"Age"`

	OriginCity         string `xml:"OriginCity"`
	OriginState        string `xml:"OriginState"`
	OriginCountry      string `xml:"OriginCountry"`
	DestinationCity    string `xml:"DestinationCity"`
	DestinationState   string `xml:"DestinationState"`
	DestinationCountry string `xml:"DestinationCountry"`

	Miles     NullInt `xml:"Miles"` // sometimes blank
	Equipment string  `xml:"Equipment"`
	LoadType  string  `xml:"LoadType"`

	Payment      NullFloat64 `xml:"Payment"` // was string → numeric but tolerant
	FuelCost     string      `xml:"FuelCost"`
	PricePerGall NullFloat64 `xml:"PricePerGall"` // may be blank

	Weight NullInt     `xml:"Weight"`
	Length NullFloat64 `xml:"Length"` // switched to tolerant float
	Width  NullFloat64 `xml:"Width"`

	PickUpDate string `xml:"PickUpDate"`

	CompanyName         string `xml:"CompanyName"`
	PointOfContactPhone string `xml:"PointOfContactPhone"`

	// optional extras — any that might be absent are tolerant
	Bond        NullInt `xml:"Bond"`
	BondEnabled bool    `xml:"BondEnabled"`
	BondTypeID  NullInt `xml:"BondTypeID"`
	// Days2Pay            string  `json:"Days2Pay"`
	// ExperienceFactor    string  `json:"ExperienceFactor"`
	OriginDistance      NullInt `xml:"OriginDistance"`
	DestinationDistance NullInt `xml:"DestinationDistance"`
	TruckCompanyID      NullInt `xml:"TruckCompanyId"`
}

// ────────────────────────────────────────────
// ENUMS
// ────────────────────────────────────────────

type LoadType string

const (
	Nothing LoadType = "Nothing"
	All     LoadType = "All"
	Full    LoadType = "Full"
	Partial LoadType = "Partial"
)

type SortColumn string

const (
	Equipment           SortColumn = "Equipment"
	LoadTypeSort        SortColumn = "LoadType"
	PickUpDate          SortColumn = "PickUpDate"
	OriginCity          SortColumn = "OriginCity"
	OriginStateSort     SortColumn = "OriginState"
	OriginDistance      SortColumn = "OriginDistance"
	DestinationCity     SortColumn = "DestinationCity"
	DestinationState    SortColumn = "DestinationState"
	Payment             SortColumn = "Payment"
	PaymentAmount       SortColumn = "PaymentAmount"
	LengthCol           SortColumn = "Length"
	WeightCol           SortColumn = "Weight"
	Days2Pay            SortColumn = "Days2Pay"
	Credit              SortColumn = "Credit"
	ExperienceFactorCol SortColumn = "ExperienceFactor"
	CompanyName         SortColumn = "CompanyName"
	TruckCompanyName    SortColumn = "TruckCompanyName"
	FuelCost            SortColumn = "FuelCost"
	MilesCol            SortColumn = "Miles"
	Mileage             SortColumn = "Mileage"
	AgeCol              SortColumn = "Age"
)

type CombinedInfo struct {
	TruckData    chrobinson.Truck
	LocationData chrobinson.PseudoLocations
}
