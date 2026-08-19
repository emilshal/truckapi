package chrobinson

import "sync"

// Sized to hold the entire posted board (~13k loads across all truck
// locations as of 2026-07) with ample headroom, so book requests never miss on
// a load we sent to the Loader. ~0.5-1KB per load => worst case ~25-50MB,
// still a hard bound on memory.
const runtimeAvailableLoadCostsMaxItems = 50000

type availableLoadCostStore struct {
	mu          sync.RWMutex
	byLoad      map[int][]AvailableLoadCost
	recentLoads []int
}

var runtimeAvailableLoadCosts = newAvailableLoadCostStore()

func newAvailableLoadCostStore() *availableLoadCostStore {
	return &availableLoadCostStore{
		byLoad:      make(map[int][]AvailableLoadCost),
		recentLoads: make([]int, 0, runtimeAvailableLoadCostsMaxItems),
	}
}

// CacheAvailableLoadCosts keeps the original CHRob search costs in memory so later
// booking requests can use the exact payload shape CHRob expects.
func CacheAvailableLoadCosts(loadNumber int, costs []AvailableLoadCost) {
	runtimeAvailableLoadCosts.put(loadNumber, costs)
}

func AvailableLoadCostsForLoadNumber(loadNumber int) ([]AvailableLoadCost, bool) {
	return runtimeAvailableLoadCosts.get(loadNumber)
}

func BookingLoadCostsForLoadNumber(loadNumber int) ([]LoadCost, bool) {
	costs, ok := AvailableLoadCostsForLoadNumber(loadNumber)
	if !ok || len(costs) == 0 {
		return nil, false
	}

	out := make([]LoadCost, 0, len(costs))
	for _, cost := range costs {
		out = append(out, LoadCost{
			Type:              cost.Type,
			Code:              cost.Code,
			Description:       cost.Description,
			SourceCostPerUnit: cost.SourceCostPerUnit,
			Units:             cost.Units,
			CurrencyCode:      cost.CurrencyCode,
		})
	}

	return out, true
}

func ResetRuntimeAvailableLoadCostsForTests() {
	runtimeAvailableLoadCosts.mu.Lock()
	defer runtimeAvailableLoadCosts.mu.Unlock()

	runtimeAvailableLoadCosts.byLoad = make(map[int][]AvailableLoadCost)
	runtimeAvailableLoadCosts.recentLoads = runtimeAvailableLoadCosts.recentLoads[:0]

	runtimePickupDefaults.mu.Lock()
	defer runtimePickupDefaults.mu.Unlock()
	runtimePickupDefaults.byLoad = make(map[int]PickupDefaults)
	runtimePickupDefaults.recent = nil
}

// PickupDefaults holds the load's origin and pickup timing captured at search
// time, so a booking that omits emptyLocation/emptyDateTime can default to the
// pickup origin (a truck arriving empty at the pickup on the pickup date).
type PickupDefaults struct {
	Origin         Location
	PickupDateTime string
}

type pickupDefaultsStore struct {
	mu     sync.RWMutex
	byLoad map[int]PickupDefaults
	recent []int
}

var runtimePickupDefaults = &pickupDefaultsStore{byLoad: make(map[int]PickupDefaults)}

// CachePickupDefaults stores the origin + pickup datetime for a load. Safe to
// call alongside CacheAvailableLoadCosts on every search result. Bounded to the
// same capacity as the cost cache so the runner's high search volume can't grow
// this map without limit.
func CachePickupDefaults(loadNumber int, origin Location, pickupDateTime string) {
	if loadNumber <= 0 {
		return
	}
	runtimePickupDefaults.mu.Lock()
	defer runtimePickupDefaults.mu.Unlock()

	if _, exists := runtimePickupDefaults.byLoad[loadNumber]; !exists {
		runtimePickupDefaults.recent = append(runtimePickupDefaults.recent, loadNumber)
		for len(runtimePickupDefaults.recent) > runtimeAvailableLoadCostsMaxItems {
			stale := runtimePickupDefaults.recent[0]
			runtimePickupDefaults.recent = runtimePickupDefaults.recent[1:]
			delete(runtimePickupDefaults.byLoad, stale)
		}
	}
	runtimePickupDefaults.byLoad[loadNumber] = PickupDefaults{Origin: origin, PickupDateTime: pickupDateTime}
}

// PickupDefaultsForLoadNumber returns cached pickup defaults for a load.
func PickupDefaultsForLoadNumber(loadNumber int) (PickupDefaults, bool) {
	runtimePickupDefaults.mu.RLock()
	defer runtimePickupDefaults.mu.RUnlock()
	d, ok := runtimePickupDefaults.byLoad[loadNumber]
	return d, ok
}

func (s *availableLoadCostStore) put(loadNumber int, costs []AvailableLoadCost) {
	if loadNumber <= 0 || len(costs) == 0 {
		return
	}

	cloned := cloneAvailableLoadCosts(costs)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.byLoad[loadNumber] = cloned
	s.promoteLoadNumberLocked(loadNumber)
	s.trimLocked()
}

func (s *availableLoadCostStore) get(loadNumber int) ([]AvailableLoadCost, bool) {
	if loadNumber <= 0 {
		return nil, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	costs, ok := s.byLoad[loadNumber]
	if !ok {
		return nil, false
	}

	return cloneAvailableLoadCosts(costs), true
}

func (s *availableLoadCostStore) promoteLoadNumberLocked(loadNumber int) {
	for i, existing := range s.recentLoads {
		if existing == loadNumber {
			copy(s.recentLoads[i:], s.recentLoads[i+1:])
			s.recentLoads = s.recentLoads[:len(s.recentLoads)-1]
			break
		}
	}
	s.recentLoads = append([]int{loadNumber}, s.recentLoads...)
}

func (s *availableLoadCostStore) trimLocked() {
	for len(s.recentLoads) > runtimeAvailableLoadCostsMaxItems {
		stale := s.recentLoads[len(s.recentLoads)-1]
		s.recentLoads = s.recentLoads[:len(s.recentLoads)-1]
		delete(s.byLoad, stale)
	}
}

func cloneAvailableLoadCosts(costs []AvailableLoadCost) []AvailableLoadCost {
	out := make([]AvailableLoadCost, len(costs))
	copy(out, costs)
	return out
}
