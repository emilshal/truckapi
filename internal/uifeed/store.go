package uifeed

import (
	"strings"
	"sync"
	"time"
	"truckapi/internal/loader"
)

type Source string

const (
	SourceCHRobinson Source = "CHROBINSON"
	SourceTruckstop  Source = "TRUCKSTOP"
)

type Item struct {
	ReceivedAt time.Time          `json:"receivedAt"`
	Order      loader.LoaderOrder `json:"order"`
}

type Page struct {
	Source   Source `json:"source"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
	Total    int    `json:"total"`
	Items    []Item `json:"items"`
}

type Store struct {
	mu       sync.RWMutex
	capacity int
	items    map[Source][]Item // oldest -> newest
}

func NewStore(capacity int) *Store {
	if capacity <= 0 {
		capacity = 500
	}
	return &Store{
		capacity: capacity,
		items: map[Source][]Item{
			SourceCHRobinson: {},
			SourceTruckstop:  {},
		},
	}
}

func ParseSource(value string) (Source, bool) {
	switch Source(strings.ToUpper(strings.TrimSpace(value))) {
	case SourceCHRobinson:
		return SourceCHRobinson, true
	case SourceTruckstop:
		return SourceTruckstop, true
	default:
		return "", false
	}
}

func (s *Store) Add(order loader.LoaderOrder) {
	source, ok := ParseSource(string(order.Source))
	if !ok {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	queue := s.items[source]
	queue = append(queue, Item{
		ReceivedAt: time.Now(),
		Order:      order,
	})

	if len(queue) > s.capacity {
		queue = queue[len(queue)-s.capacity:]
	}

	s.items[source] = queue
}

func (s *Store) List(source Source, page, pageSize int) Page {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > 200 {
		pageSize = 200
	}

	s.mu.RLock()
	queue := s.items[source]
	total := len(queue)
	s.mu.RUnlock()

	// Newest-first pagination, without reversing the whole slice.
	startFromEnd := (page - 1) * pageSize
	endFromEnd := startFromEnd + pageSize
	if startFromEnd >= total {
		return Page{
			Source:   source,
			Page:     page,
			PageSize: pageSize,
			Total:    total,
			Items:    []Item{},
		}
	}
	if endFromEnd > total {
		endFromEnd = total
	}

	items := make([]Item, 0, endFromEnd-startFromEnd)
	// Indices are from the end: newest is total-1.
	for i := total - 1 - startFromEnd; i >= total-endFromEnd; i-- {
		items = append(items, queue[i])
	}

	return Page{
		Source:   source,
		Page:     page,
		PageSize: pageSize,
		Total:    total,
		Items:    items,
	}
}
