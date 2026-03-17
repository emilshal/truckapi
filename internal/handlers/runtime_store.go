package handlers

import (
	"sync"
	"time"
	"truckapi/internal/chrobinson"
)

const runtimeStoreMaxItems = 500

type runtimeTrackingStore struct {
	mu              sync.RWMutex
	nextOfferID     uint
	nextBookingID   uint
	nextShipmentID  uint
	offers          []chrobinson.OfferResponse
	bookings        []chrobinson.LoadBookingRecord
	shipmentDetails []chrobinson.ShipmentDetailsRecord
}

var runtimeStore = newRuntimeTrackingStore()

func newRuntimeTrackingStore() *runtimeTrackingStore {
	return &runtimeTrackingStore{
		nextOfferID:     1,
		nextBookingID:   1,
		nextShipmentID:  1,
		offers:          make([]chrobinson.OfferResponse, 0, runtimeStoreMaxItems),
		bookings:        make([]chrobinson.LoadBookingRecord, 0, runtimeStoreMaxItems),
		shipmentDetails: make([]chrobinson.ShipmentDetailsRecord, 0, runtimeStoreMaxItems),
	}
}

func resetRuntimeStoreForTests() {
	runtimeStore.mu.Lock()
	defer runtimeStore.mu.Unlock()

	runtimeStore.nextOfferID = 1
	runtimeStore.nextBookingID = 1
	runtimeStore.nextShipmentID = 1
	runtimeStore.offers = runtimeStore.offers[:0]
	runtimeStore.bookings = runtimeStore.bookings[:0]
	runtimeStore.shipmentDetails = runtimeStore.shipmentDetails[:0]
}

func (s *runtimeTrackingStore) upsertOffer(record chrobinson.OfferResponse) chrobinson.OfferResponse {
	s.mu.Lock()
	defer s.mu.Unlock()

	if record.CreatedAt == "" {
		record.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if record.UpdatedAt == "" {
		record.UpdatedAt = record.CreatedAt
	}

	for i, existing := range s.offers {
		if existing.OfferRequestId != "" && existing.OfferRequestId == record.OfferRequestId {
			record.ID = existing.ID
			s.offers = append([]chrobinson.OfferResponse{record}, append(s.offers[:i], s.offers[i+1:]...)...)
			return record
		}
	}

	record.ID = s.nextOfferID
	s.nextOfferID++
	s.offers = append([]chrobinson.OfferResponse{record}, s.offers...)
	if len(s.offers) > runtimeStoreMaxItems {
		s.offers = s.offers[:runtimeStoreMaxItems]
	}
	return record
}

func (s *runtimeTrackingStore) listOffers() []chrobinson.OfferResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]chrobinson.OfferResponse, len(s.offers))
	copy(out, s.offers)
	return out
}

func (s *runtimeTrackingStore) addBooking(record chrobinson.LoadBookingRecord) chrobinson.LoadBookingRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	record.ID = s.nextBookingID
	s.nextBookingID++
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = now
	}
	s.bookings = append([]chrobinson.LoadBookingRecord{record}, s.bookings...)
	if len(s.bookings) > runtimeStoreMaxItems {
		s.bookings = s.bookings[:runtimeStoreMaxItems]
	}
	return record
}

func (s *runtimeTrackingStore) listBookings() []chrobinson.LoadBookingRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]chrobinson.LoadBookingRecord, len(s.bookings))
	copy(out, s.bookings)
	return out
}

func (s *runtimeTrackingStore) addShipmentDetails(record chrobinson.ShipmentDetailsRecord) chrobinson.ShipmentDetailsRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	record.ID = s.nextShipmentID
	s.nextShipmentID++
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = now
	}
	s.shipmentDetails = append([]chrobinson.ShipmentDetailsRecord{record}, s.shipmentDetails...)
	if len(s.shipmentDetails) > runtimeStoreMaxItems {
		s.shipmentDetails = s.shipmentDetails[:runtimeStoreMaxItems]
	}
	return record
}

func (s *runtimeTrackingStore) listShipmentDetails() []chrobinson.ShipmentDetailsRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]chrobinson.ShipmentDetailsRecord, len(s.shipmentDetails))
	copy(out, s.shipmentDetails)
	return out
}
