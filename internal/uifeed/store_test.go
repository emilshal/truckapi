package uifeed

import (
	"testing"
	"truckapi/internal/loader"
)

func TestStore_ListNewestFirstAndCapacity(t *testing.T) {
	store := NewStore(3)

	store.Add(loader.LoaderOrder{Source: string(SourceCHRobinson), OrderNumber: "A"})
	store.Add(loader.LoaderOrder{Source: string(SourceCHRobinson), OrderNumber: "B"})
	store.Add(loader.LoaderOrder{Source: string(SourceCHRobinson), OrderNumber: "C"})
	store.Add(loader.LoaderOrder{Source: string(SourceCHRobinson), OrderNumber: "D"}) // pushes out A

	page := store.List(SourceCHRobinson, 1, 2)
	if page.Total != 3 {
		t.Fatalf("expected total=3, got %d", page.Total)
	}
	if len(page.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(page.Items))
	}
	if page.Items[0].Order.OrderNumber != "D" || page.Items[1].Order.OrderNumber != "C" {
		t.Fatalf("expected newest-first [D,C], got [%s,%s]", page.Items[0].Order.OrderNumber, page.Items[1].Order.OrderNumber)
	}

	page2 := store.List(SourceCHRobinson, 2, 2)
	if len(page2.Items) != 1 {
		t.Fatalf("expected 1 item on page 2, got %d", len(page2.Items))
	}
	if page2.Items[0].Order.OrderNumber != "B" {
		t.Fatalf("expected oldest remaining to be B, got %s", page2.Items[0].Order.OrderNumber)
	}
}
