package tdmonitor

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/plugin"
)

func TestLinkedMonitorStartsAndSchedulesRemoteSync(t *testing.T) {
	projectRoot := findProjectRootWithDB(t)
	called := make(chan string, 1)
	p := New()
	p.remoteSyncCommand = func(_ context.Context, workDir string) error {
		called <- workDir
		return nil
	}
	p.ctx = &plugin.Context{
		WorkDir: projectRoot,
		Epoch:   7,
		Logger:  slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}
	if err := p.Init(p.ctx); err != nil {
		t.Fatal(err)
	}
	p.tdOnPath = true
	defer p.Stop()

	build := p.Start()
	ready := build().(MonitorReadyMsg)
	if err := ready.Model.DB.SetSyncState("p_test"); err != nil {
		t.Fatalf("link test database: %v", err)
	}

	_, start := p.Update(ready)
	if start == nil {
		t.Fatal("adopting linked monitor scheduled no commands")
	}
	if p.model.AutoSyncFunc == nil || p.model.AutoSyncInterval != remoteSyncInterval || p.model.LastAutoSync.IsZero() {
		t.Fatalf("periodic sync = (%v, %v), want callback and %v",
			p.model.AutoSyncFunc != nil, p.model.AutoSyncInterval, remoteSyncInterval)
	}

	batch, ok := start().(tea.BatchMsg)
	if !ok {
		t.Fatalf("start produced %T, want tea.BatchMsg", start())
	}
	var finished RemoteSyncFinishedMsg
	for _, cmd := range batch {
		if msg, ok := cmd().(RemoteSyncFinishedMsg); ok {
			finished = msg
		}
	}
	if finished.runner == nil {
		t.Fatal("linked monitor did not schedule an immediate remote pull")
	}
	if finished.Epoch != 7 || finished.Err != nil {
		t.Fatalf("remote sync result = %#v", finished)
	}
	select {
	case got := <-called:
		if got != projectRoot {
			t.Fatalf("sync workdir = %q, want %q", got, projectRoot)
		}
	default:
		t.Fatal("immediate remote pull did not run")
	}
}

func TestUnlinkedMonitorDoesNotStartRemoteSync(t *testing.T) {
	p := New()
	p.remoteSyncCommand = func(context.Context, string) error {
		t.Fatal("unlinked monitor attempted remote sync")
		return nil
	}
	p.ctx = &plugin.Context{
		WorkDir: findProjectRootWithDB(t),
		Logger:  slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}
	if err := p.Init(p.ctx); err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	startAndSettle(t, p)

	if p.remoteSync != nil || p.model.AutoSyncFunc != nil || p.model.AutoSyncInterval != 0 {
		t.Fatal("unlinked monitor configured remote sync")
	}
}

func TestRemoteSyncRunnerIsSingleFlight(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	runner := newRemoteSyncRunner("/test/project", func(context.Context, string) error {
		calls.Add(1)
		close(entered)
		<-release
		return nil
	})
	defer runner.stop()

	done := make(chan error, 1)
	go func() { done <- runner.pull() }()
	<-entered
	if err := runner.pull(); err != nil {
		t.Fatalf("overlapping pull returned error: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("overlapping pulls executed %d commands, want 1", got)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRemoteSyncRunnerStopCancelsProjectPull(t *testing.T) {
	entered := make(chan struct{})
	runner := newRemoteSyncRunner("/old/project", func(ctx context.Context, _ string) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	})

	done := make(chan error, 1)
	go func() { done <- runner.pull() }()
	<-entered
	runner.stop()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("stopped pull returned %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stopped project pull was not canceled")
	}
}
