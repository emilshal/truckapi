package chrobinson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// FlexibleInt accepts either a JSON number or a quoted integer string.
type FlexibleInt int

func (f *FlexibleInt) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*f = 0
		return nil
	}
	if trimmed[0] == '"' {
		var raw string
		if err := json.Unmarshal(trimmed, &raw); err != nil {
			return err
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			*f = 0
			return nil
		}
		value, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("invalid integer string %q: %w", raw, err)
		}
		*f = FlexibleInt(value)
		return nil
	}

	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return err
	}
	value, err := strconv.Atoi(number.String())
	if err != nil {
		return fmt.Errorf("invalid integer %q: %w", number.String(), err)
	}
	*f = FlexibleInt(value)
	return nil
}

func (f FlexibleInt) Int() int {
	return int(f)
}

// FlexibleString accepts either a JSON string or a JSON number and normalizes it to a string.
type FlexibleString string

func (f *FlexibleString) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*f = ""
		return nil
	}
	if trimmed[0] == '"' {
		var raw string
		if err := json.Unmarshal(trimmed, &raw); err != nil {
			return err
		}
		*f = FlexibleString(strings.TrimSpace(raw))
		return nil
	}

	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return err
	}
	*f = FlexibleString(number.String())
	return nil
}

func (f FlexibleString) String() string {
	return string(f)
}

// OfferResponseCallback matches CHRob's callback schema, which may serialize numeric fields as strings.
type OfferResponseCallback struct {
	LoadNumber     FlexibleInt `json:"loadNumber"`
	CarrierCode    string      `json:"carrierCode"`
	OfferRequestId string      `json:"offerRequestId"`
	OfferId        FlexibleInt `json:"offerId"`
	OfferResult    string      `json:"offerResult"`
	Price          FlexibleInt `json:"price"`
	CurrencyCode   string      `json:"currencyCode"`
	RejectReasons  []string    `json:"rejectReasons"`
}

// ShipmentDetailsCallback matches CHRob's shipment details callback schema.
type ShipmentDetailsCallback struct {
	Time        string               `json:"time"`
	CarrierCode string               `json:"carrierCode"`
	Scac        string               `json:"scac"`
	LoadNumber  FlexibleString       `json:"loadNumber"`
	ClientId    string               `json:"clientId"`
	EventTime   string               `json:"eventTime"`
	Event       ShipmentDetailsEvent `json:"event"`
}

type ShipmentDetailsEvent struct {
	EventType           string         `json:"eventType"`
	EventSubType        string         `json:"eventSubType"`
	LoadNumber          FlexibleString `json:"loadNumber"`
	Rate                float64        `json:"rate"`
	ActivityDate        string         `json:"activityDate"`
	PickUpByDate        string         `json:"pickUpByDate"`
	PickUpReadyByDate   string         `json:"pickUpReadyByDate"`
	DeliverByDate       string         `json:"deliverByDate"`
	DeliveryReadyByDate string         `json:"deliveryReadyByDate"`
	Mode                string         `json:"mode"`
	TotalCommodityValue float64        `json:"totalCommodityValue"`
	Notes               string         `json:"notes"`
	SourceUnitOfMeasure string         `json:"sourceUnitOfMeasure"`
	ChargeableWeight    float64        `json:"chargeableWeight"`
}

// ShipmentDetailsRecord stores received shipment detail callbacks for audit/tracing.
type ShipmentDetailsRecord struct {
	ID           uint   `gorm:"primaryKey"`
	LoadNumber   string `gorm:"index"`
	CarrierCode  string `gorm:"index"`
	Scac         string
	ClientID     string
	CallbackTime string
	EventTime    string
	EventType    string `gorm:"index"`
	EventSubType string
	Mode         string
	ActivityDate string
	RawPayload   string `gorm:"type:text"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// LoadBookingRecord stores outbound booking attempts so they can be reviewed later.
type LoadBookingRecord struct {
	ID                    uint      `gorm:"primaryKey" json:"id"`
	LoadNumber            int       `gorm:"index" json:"loadNumber"`
	CarrierCode           string    `gorm:"index" json:"carrierCode"`
	Status                string    `gorm:"index" json:"status"`
	EmptyDateTime         string    `json:"emptyDateTime"`
	RateConfirmationName  string    `json:"rateConfirmationName"`
	RateConfirmationEmail string    `json:"rateConfirmationEmail"`
	AvailableLoadCosts    string    `gorm:"type:text" json:"availableLoadCosts"`
	RawRequest            string    `gorm:"type:text" json:"rawRequest"`
	CreatedAt             time.Time `json:"createdAt"`
	UpdatedAt             time.Time `json:"updatedAt"`
}
