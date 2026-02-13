package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// SyncResult summarizes the outcome of a sync operation.
type SyncResult struct {
	Pulled int      // Issues created/updated locally
	Pushed int      // Issues created/updated on GitHub
	Errors []string // Non-fatal errors
}

// Pull fetches issues from the external provider and creates/updates local td issues.
func Pull(ctx context.Context, provider Provider, workDir, todosDir string) (*SyncResult, error) {
	result := &SyncResult{}

	// Get all external issues
	extIssues, err := provider.List(ctx, workDir)
	if err != nil {
		return nil, fmt.Errorf("list external issues: %w", err)
	}

	// Get all local td issues
	tdIssues, err := listTDIssues(workDir)
	if err != nil {
		return nil, fmt.Errorf("list td issues: %w", err)
	}

	// Load sync state
	state, err := LoadState(todosDir)
	if err != nil {
		return nil, fmt.Errorf("load sync state: %w", err)
	}

	// Build lookup for td issues by ID
	tdByID := make(map[string]TDIssue, len(tdIssues))
	for _, td := range tdIssues {
		tdByID[td.ID] = td
	}

	for _, ext := range extIssues {
		ghNumber, err := strconv.Atoi(ext.ID)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("invalid GH number %q", ext.ID))
			continue
		}

		tdID, entry := state.FindByGHNumber(ghNumber)

		if entry != nil {
			// Already mapped — update if GH changed since last sync
			if ext.UpdatedAt.After(entry.GHUpdatedAt) {
				mapped := ExternalToTD(ext)
				if err := updateTDIssue(workDir, tdID, mapped); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("update td %s: %v", tdID, err))
					continue
				}
				// Update sync state timestamps
				state.Issues[tdID] = SyncStateEntry{
					GHNumber:    ghNumber,
					TDUpdatedAt: time.Now(),
					GHUpdatedAt: ext.UpdatedAt,
				}
				result.Pulled++
			}
		} else {
			// New issue — create in td
			mapped := ExternalToTD(ext)
			newID, err := createTDIssue(workDir, mapped)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("create td for GH #%d: %v", ghNumber, err))
				continue
			}
			state.Issues[newID] = SyncStateEntry{
				GHNumber:    ghNumber,
				TDUpdatedAt: time.Now(),
				GHUpdatedAt: ext.UpdatedAt,
			}
			result.Pulled++
		}
	}

	// Save sync state
	if err := SaveState(todosDir, state); err != nil {
		return result, fmt.Errorf("save sync state: %w", err)
	}

	return result, nil
}

// Push sends local td issues to the external provider.
func Push(ctx context.Context, provider Provider, workDir, todosDir string) (*SyncResult, error) {
	result := &SyncResult{}

	// Get all local td issues
	tdIssues, err := listTDIssues(workDir)
	if err != nil {
		return nil, fmt.Errorf("list td issues: %w", err)
	}

	// Load sync state
	state, err := LoadState(todosDir)
	if err != nil {
		return nil, fmt.Errorf("load sync state: %w", err)
	}

	for _, td := range tdIssues {
		entry := state.FindByTDID(td.ID)
		ext := TDToExternal(td)

		if entry != nil {
			// Already mapped — update if td changed since last sync
			if td.UpdatedAt.After(entry.TDUpdatedAt) {
				ghID := strconv.Itoa(entry.GHNumber)
				if err := provider.Update(ctx, workDir, ghID, ext); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("update GH #%d: %v", entry.GHNumber, err))
					continue
				}
				// Handle state changes (close/reopen)
				if ext.State == ghStateClosed {
					if err := provider.Close(ctx, workDir, ghID); err != nil {
						result.Errors = append(result.Errors, fmt.Sprintf("close GH #%d: %v", entry.GHNumber, err))
					}
				} else {
					// Reopen if needed — gh reopen on already-open issue is a no-op
					if err := provider.Reopen(ctx, workDir, ghID); err != nil {
						// Non-fatal: issue might already be open
						_ = err
					}
				}
				state.Issues[td.ID] = SyncStateEntry{
					GHNumber:    entry.GHNumber,
					TDUpdatedAt: td.UpdatedAt,
					GHUpdatedAt: time.Now(),
				}
				result.Pushed++
			}
		} else {
			// New issue — create on GH
			newID, err := provider.Create(ctx, workDir, ext)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("create GH for %s: %v", td.ID, err))
				continue
			}
			ghNumber, _ := strconv.Atoi(newID)

			// If the issue is closed, close it on GH too
			if ext.State == ghStateClosed {
				if err := provider.Close(ctx, workDir, newID); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("close GH #%s: %v", newID, err))
				}
			}

			state.Issues[td.ID] = SyncStateEntry{
				GHNumber:    ghNumber,
				TDUpdatedAt: td.UpdatedAt,
				GHUpdatedAt: time.Now(),
			}
			result.Pushed++
		}
	}

	// Save sync state
	if err := SaveState(todosDir, state); err != nil {
		return result, fmt.Errorf("save sync state: %w", err)
	}

	return result, nil
}

// listTDIssues runs td list --json and returns parsed issues.
func listTDIssues(workDir string) ([]TDIssue, error) {
	cmd := exec.Command("td", "list", "--json")
	cmd.Dir = workDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return nil, fmt.Errorf("td list --json: %s", errMsg)
	}

	var issues []TDIssue
	if err := json.Unmarshal(output, &issues); err != nil {
		return nil, fmt.Errorf("parse td issues: %w", err)
	}

	return issues, nil
}

// createTDIssue creates a new td issue and returns its ID.
func createTDIssue(workDir string, td TDIssue) (string, error) {
	args := []string{"create", td.Title}

	if td.Description != "" {
		args = append(args, "--description", td.Description)
	}
	if td.Type != "" {
		args = append(args, "--type", td.Type)
	}
	if td.Priority != "" {
		args = append(args, "--priority", td.Priority)
	}
	if len(td.Labels) > 0 {
		args = append(args, "--labels", strings.Join(td.Labels, ","))
	}

	cmd := exec.Command("td", args...)
	cmd.Dir = workDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return "", fmt.Errorf("td create: %s", errMsg)
	}

	// td create outputs "Created issue <id>" — extract the ID
	outStr := strings.TrimSpace(string(output))
	// Look for a td-xxxxxx pattern in the output
	for _, word := range strings.Fields(outStr) {
		if strings.HasPrefix(word, "td-") {
			return word, nil
		}
	}

	// Fallback: return the last word
	fields := strings.Fields(outStr)
	if len(fields) > 0 {
		return fields[len(fields)-1], nil
	}

	return "", fmt.Errorf("could not parse issue ID from: %s", outStr)
}

// updateTDIssue updates an existing td issue.
func updateTDIssue(workDir string, tdID string, td TDIssue) error {
	args := []string{"update", tdID}

	if td.Title != "" {
		args = append(args, "--title", td.Title)
	}
	if td.Description != "" {
		args = append(args, "--description", td.Description)
	}
	if td.Status != "" {
		args = append(args, "--status", td.Status)
	}
	if td.Type != "" {
		args = append(args, "--type", td.Type)
	}
	if td.Priority != "" {
		args = append(args, "--priority", td.Priority)
	}
	if len(td.Labels) > 0 {
		args = append(args, "--labels", strings.Join(td.Labels, ","))
	}

	cmd := exec.Command("td", args...)
	cmd.Dir = workDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return fmt.Errorf("td update %s: %s", tdID, errMsg)
	}

	return nil
}
