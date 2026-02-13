package trifectaindex

import (
	"slices"
	"strings"
	"time"
)

type ActiveWOSelection struct {
	ActiveWO          *WorkOrder
	RunningCount      int
	ExtraRunningCount int
	Reason            string
}

// SelectActiveWorkOrder picks one running WO deterministically:
// priority desc -> recency desc -> id asc.
func SelectActiveWorkOrder(workOrders []WorkOrder) ActiveWOSelection {
	running := make([]WorkOrder, 0)
	for _, wo := range workOrders {
		if wo.Status == WOStatusRunning {
			running = append(running, wo)
		}
	}

	selection := ActiveWOSelection{
		RunningCount:      len(running),
		ExtraRunningCount: max(len(running)-1, 0),
	}
	if len(running) == 0 {
		selection.Reason = "no_running_work_orders"
		return selection
	}

	slices.SortFunc(running, func(a, b WorkOrder) int {
		pa, pb := priorityScore(a.Priority), priorityScore(b.Priority)
		if pa != pb {
			return pb - pa
		}
		ta, tb := recency(a), recency(b)
		if !ta.Equal(tb) {
			if ta.After(tb) {
				return -1
			}
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})

	active := running[0]
	selection.ActiveWO = &active
	selection.Reason = "priority_then_recency_then_id"
	return selection
}

func priorityScore(priority string) int {
	switch strings.ToLower(strings.TrimSpace(priority)) {
	case "p0", "critical":
		return 4
	case "p1", "high":
		return 3
	case "p2", "medium":
		return 2
	case "p3", "low":
		return 1
	default:
		return 0
	}
}

func recency(wo WorkOrder) time.Time {
	if !wo.CreatedAt.IsZero() {
		return wo.CreatedAt
	}
	if !wo.ClosedAt.IsZero() {
		return wo.ClosedAt
	}
	return time.Time{}
}
