package tdmonitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
)

const remoteSyncInterval = time.Minute

type remoteSyncCommand func(context.Context, string) error

// remoteSyncRunner owns one project's pull lifecycle. A project switch cancels
// the old context, and the atomic guard prevents the startup pull and periodic
// monitor callback from running td concurrently against the same database.
type remoteSyncRunner struct {
	ctx      context.Context
	cancel   context.CancelFunc
	workDir  string
	run      remoteSyncCommand
	inFlight atomic.Bool
}

func newRemoteSyncRunner(workDir string, run remoteSyncCommand) *remoteSyncRunner {
	ctx, cancel := context.WithCancel(context.Background())
	return &remoteSyncRunner{
		ctx:     ctx,
		cancel:  cancel,
		workDir: workDir,
		run:     run,
	}
}

func (r *remoteSyncRunner) pull() error {
	if r == nil || r.run == nil {
		return nil
	}
	if !r.inFlight.CompareAndSwap(false, true) {
		return nil
	}
	defer r.inFlight.Store(false)
	return r.run(r.ctx, r.workDir)
}

func (r *remoteSyncRunner) stop() {
	if r != nil && r.cancel != nil {
		r.cancel()
	}
}

func runTDRemoteSync(ctx context.Context, workDir string) error {
	statusCmd := exec.CommandContext(ctx, "td", "-w", workDir, "--json", "sync", "status")
	statusOut, err := statusCmd.CombinedOutput()
	if err != nil {
		return commandError(ctx, "td sync status", statusOut, err)
	}
	var status struct {
		Gate          string `json:"gate"`
		Configured    bool   `json:"configured"`
		Authenticated bool   `json:"authenticated"`
	}
	if err := json.Unmarshal(statusOut, &status); err != nil {
		return fmt.Errorf("td sync status: decode JSON: %w", err)
	}
	if status.Gate != "ON" || !status.Configured || !status.Authenticated {
		return nil
	}

	cmd := exec.CommandContext(ctx, "td", "-w", workDir, "sync", "--pull")
	// Sync's CLI surface is feature-gated even though linked projects autosync
	// by default. Sidecar is deliberately invoking that integration, so enable
	// the child command without requiring Sidecar's parent shell to export it.
	cmd.Env = append(os.Environ(), "TD_FEATURE_SYNC_CLI=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	return commandError(ctx, "td sync --pull", out, err)
}

func commandError(ctx context.Context, name string, out []byte, err error) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return context.Canceled
	}
	detail := strings.TrimSpace(string(out))
	if detail == "" {
		return fmt.Errorf("%s: %w", name, err)
	}
	return fmt.Errorf("%s: %w: %s", name, err, detail)
}

// RemoteSyncFinishedMsg lets the plugin promptly detect a snapshot replacement
// after the startup pull. Ordinary in-place changes are picked up by the
// monitor's existing local refresh tick without creating a second poll chain.
type RemoteSyncFinishedMsg struct {
	Epoch  uint64
	runner *remoteSyncRunner
	Err    error
}

func (m RemoteSyncFinishedMsg) GetEpoch() uint64 { return m.Epoch }

func remoteSyncCmd(epoch uint64, runner *remoteSyncRunner) tea.Cmd {
	return func() tea.Msg {
		return RemoteSyncFinishedMsg{Epoch: epoch, runner: runner, Err: runner.pull()}
	}
}
