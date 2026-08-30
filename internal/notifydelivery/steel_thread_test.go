package notifydelivery

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/notify"
)

// This fast in-process complement keeps the shared-ledger policy behavior
// local to notifydelivery. The OS-process and app-wiring acceptance proof lives
// in internal/app/notification_delivery_process_test.go.
func TestTwoServiceSteelThreadDeliversOnceAndRetainsCentreRecords(t *testing.T) {
	stateDir := t.TempDir()
	ledgerPath := filepath.Join(stateDir, LedgerFileName)
	native := &fakeNative{capability: Capability{Available: true, Provider: "fake-native"}}
	sound := &fakeSound{capability: Capability{Available: true, Provider: "fake-sound"}}
	// Anchored to the wall clock, not a literal date: this is the one delivery
	// test backed by the JSONL ledger, and OpenPath garbage-collects entries
	// against time.Now() as it loads them. A pinned date stops matching the
	// real clock the moment it falls more than ReceiptRetention in the past,
	// at which point the first service's receipt is swept away before the
	// second service reads the file and both deliver.
	now := time.Now().UTC()
	cfg := config.DefaultNotificationsConfig()
	cfg.Native.Mode, cfg.Sound.Mode = config.DeliveryBackground, config.DeliveryBackground
	policy := notify.ResolveConfig(cfg)

	newService := func(owner string, foreground bool) *Service {
		return NewService(ServiceOptions{
			Native: native, Sound: sound,
			Ledger:    func() (Ledger, error) { return OpenPath(ledgerPath) },
			Attention: fakeAttention{foreground: foreground},
			Config:    func() notify.ResolvedConfig { return policy },
			Clock:     fixedClock{now: now}, Owner: owner,
		})
	}
	one, two := newService("process-one", false), newService("process-two", false)
	store, err := notify.Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close notification store: %v", err)
		}
	})
	posted, err := store.Post(notify.Notification{
		ID: "ntf-background-steel-thread", Source: notify.SourceWaiting,
		Severity: notify.SeverityWarning, Title: "Agent needs input", CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	results := make(chan Result, 2)
	var wg sync.WaitGroup
	for _, service := range []*Service{one, two} {
		wg.Add(1)
		go func(service *Service) {
			defer wg.Done()
			results <- service.Deliver(context.Background(), Request{Notification: posted.Notification})
		}(service)
	}
	wg.Wait()
	close(results)
	nativeDelivered, soundDelivered := 0, 0
	for result := range results {
		if result.Native.Delivered {
			nativeDelivered++
		}
		if result.Sound.Delivered {
			soundDelivered++
		}
	}
	if nativeDelivered != 1 || soundDelivered != 1 || len(native.delivered) != 1 || len(sound.played) != 1 {
		t.Fatalf("delivered native=%d sound=%d provider calls native=%d sound=%d", nativeDelivered, soundDelivered, len(native.delivered), len(sound.played))
	}
	if records, err := store.List(); err != nil || len(records) != 1 || records[0].ID != posted.ID {
		t.Fatalf("centre records=%+v err=%v", records, err)
	}

	// The same configured modes are silent when any live host resolves the
	// origin as foreground, while the centre record remains authoritative.
	visible := newService("visible-process", true)
	foreground, err := store.Post(notify.Notification{
		ID: "ntf-foreground-steel-thread", Source: notify.SourceWaiting,
		Severity: notify.SeverityWarning, Title: "Visible agent needs input", CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	result := visible.Deliver(context.Background(), Request{Notification: foreground.Notification})
	if result.Native.Reason != notify.ReasonForeground || result.Sound.Reason != notify.ReasonForeground {
		t.Fatalf("foreground result=%+v", result)
	}
	if len(native.delivered) != 1 || len(sound.played) != 1 {
		t.Fatal("foreground delivery invoked a provider")
	}
	if records, err := store.List(); err != nil || len(records) != 2 {
		t.Fatalf("foreground centre record missing: %+v err=%v", records, err)
	}
}
