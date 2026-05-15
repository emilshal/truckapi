package chrobinson

import "sync"

const runtimeAvailableLoadCostsMaxItems = 500

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
