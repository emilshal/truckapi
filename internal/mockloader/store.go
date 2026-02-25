package mockloader

import (
	"sort"
	"sync"
	"time"
	"truckapi/internal/loader"
)

type ReceivedOrder struct {
	ReceivedAt  time.Time          `json:"receivedAt"`
	Order       loader.LoaderOrder `json:"order"`
	CountForKey int                `json:"countForKey"`
	Duplicate   bool               `json:"duplicate"`
}

type OrderCount struct {
	OrderNumber string `json:"orderNumber"`
	Count       int    `json:"count"`
}

type Summary struct {
	TotalReceived      int            `json:"totalReceived"`
	UniqueOrderNumbers int            `json:"uniqueOrderNumbers"`
	DuplicateReceives  int            `json:"duplicateReceives"`
	BySource           map[string]int `json:"bySource"`
	TopDuplicates      []OrderCount   `json:"topDuplicates"`
	LastReceivedAt     string         `json:"lastReceivedAt,omitempty"`
}

type ListPage struct {
	Page     int             `json:"page"`
	PageSize int             `json:"pageSize"`
	Total    int             `json:"total"`
	Items    []ReceivedOrder `json:"items"`
}

type Store struct {
	mu       sync.RWMutex
	capacity int
	items    []ReceivedOrder
	counts   map[string]int
	bySource map[string]int
}

func NewStore(capacity int) *Store {
	if capacity <= 0 {
		capacity = 20000
	}
	return &Store{
		capacity: capacity,
		items:    make([]ReceivedOrder, 0, capacity),
		counts:   make(map[string]int),
		bySource: make(map[string]int),
	}
}

func (s *Store) Add(order loader.LoaderOrder) ReceivedOrder {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := order.OrderNumber
	if key == "" {
		key = "EMPTY_ORDER_NUMBER"
	}

	s.counts[key]++
	count := s.counts[key]
	received := ReceivedOrder{
		ReceivedAt:  time.Now(),
		Order:       order,
		CountForKey: count,
		Duplicate:   count > 1,
	}
	s.items = append(s.items, received)
	s.bySource[order.Source]++

	if len(s.items) > s.capacity {
		dropped := s.items[0]
		s.items = s.items[1:]

		droppedKey := dropped.Order.OrderNumber
		if droppedKey == "" {
			droppedKey = "EMPTY_ORDER_NUMBER"
		}
		if current := s.counts[droppedKey]; current > 1 {
			s.counts[droppedKey] = current - 1
		} else {
			delete(s.counts, droppedKey)
		}
		if current := s.bySource[dropped.Order.Source]; current > 1 {
			s.bySource[dropped.Order.Source] = current - 1
		} else {
			delete(s.bySource, dropped.Order.Source)
		}
	}

	return received
}

func (s *Store) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = s.items[:0]
	s.counts = make(map[string]int)
	s.bySource = make(map[string]int)
}

func (s *Store) Summary(topN int) Summary {
	if topN <= 0 {
		topN = 20
	}
	if topN > 100 {
		topN = 100
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	summary := Summary{
		TotalReceived:      len(s.items),
		UniqueOrderNumbers: len(s.counts),
		BySource:           make(map[string]int, len(s.bySource)),
	}
	for k, v := range s.bySource {
		summary.BySource[k] = v
	}
	if len(s.items) > 0 {
		summary.LastReceivedAt = s.items[len(s.items)-1].ReceivedAt.Format(time.RFC3339)
	}

	dupes := make([]OrderCount, 0, len(s.counts))
	for orderNumber, count := range s.counts {
		if count > 1 {
			dupes = append(dupes, OrderCount{
				OrderNumber: orderNumber,
				Count:       count,
			})
			summary.DuplicateReceives += count - 1
		}
	}
	sort.Slice(dupes, func(i, j int) bool {
		if dupes[i].Count == dupes[j].Count {
			return dupes[i].OrderNumber < dupes[j].OrderNumber
		}
		return dupes[i].Count > dupes[j].Count
	})
	if len(dupes) > topN {
		dupes = dupes[:topN]
	}
	summary.TopDuplicates = dupes

	return summary
}

func (s *Store) List(page, pageSize int) ListPage {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 500 {
		pageSize = 500
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	total := len(s.items)
	startFromEnd := (page - 1) * pageSize
	endFromEnd := startFromEnd + pageSize
	if startFromEnd >= total {
		return ListPage{
			Page:     page,
			PageSize: pageSize,
			Total:    total,
			Items:    []ReceivedOrder{},
		}
	}
	if endFromEnd > total {
		endFromEnd = total
	}

	items := make([]ReceivedOrder, 0, endFromEnd-startFromEnd)
	for i := total - 1 - startFromEnd; i >= total-endFromEnd; i-- {
		items = append(items, s.items[i])
	}

	return ListPage{
		Page:     page,
		PageSize: pageSize,
		Total:    total,
		Items:    items,
	}
}

var DefaultStore = NewStore(50000)
