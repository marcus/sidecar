package trifectaindex

import (
	"testing"
	"time"
)

func TestSelectActiveWorkOrder_DeterministicPriorityAndRecency(t *testing.T) {
	now := time.Now().UTC()
	older := now.Add(-time.Hour)

	workOrders := []WorkOrder{
		{ID: "WO-2", Status: WOStatusRunning, Priority: "P2", CreatedAt: now},
		{ID: "WO-1", Status: WOStatusRunning, Priority: "P1", CreatedAt: older},
		{ID: "WO-3", Status: WOStatusRunning, Priority: "P1", CreatedAt: now},
		{ID: "WO-9", Status: WOStatusPending, Priority: "P0", CreatedAt: now},
	}

	sel := SelectActiveWorkOrder(workOrders)
	if sel.ActiveWO == nil {
		t.Fatalf("expected active WO, got nil")
	}
	if sel.ActiveWO.ID != "WO-3" {
		t.Fatalf("expected WO-3, got %s", sel.ActiveWO.ID)
	}
	if sel.RunningCount != 3 {
		t.Fatalf("expected running count 3, got %d", sel.RunningCount)
	}
	if sel.ExtraRunningCount != 2 {
		t.Fatalf("expected extra count 2, got %d", sel.ExtraRunningCount)
	}
}

func TestSelectActiveWorkOrder_TieBreakByID(t *testing.T) {
	now := time.Now().UTC()
	workOrders := []WorkOrder{
		{ID: "WO-B", Status: WOStatusRunning, Priority: "P1", CreatedAt: now},
		{ID: "WO-A", Status: WOStatusRunning, Priority: "P1", CreatedAt: now},
	}

	sel := SelectActiveWorkOrder(workOrders)
	if sel.ActiveWO == nil {
		t.Fatalf("expected active WO, got nil")
	}
	if sel.ActiveWO.ID != "WO-A" {
		t.Fatalf("expected WO-A by ID tiebreak, got %s", sel.ActiveWO.ID)
	}
}
