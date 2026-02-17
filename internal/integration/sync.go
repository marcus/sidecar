package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// SyncResult summarizes the outcome of a sync operation.
type SyncResult struct {
	Pulled int      // Issues created/updated locally
	Pushed int      // Issues created/updated on external provider
	Errors []string // Non-fatal errors
}

// Pull fetches issues from the external provider and creates/updates local td issues.
func Pull(ctx context.Context, provider Provider, workDir, todosDir string) (*SyncResult, error) {
	result := &SyncResult{}
	mapper := provider.Mapper()

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
	state, err := LoadState(todosDir, provider.ID())
	if err != nil {
		return nil, fmt.Errorf("load sync state: %w", err)
	}

	// Build lookup for td issues by ID
	tdByID := make(map[string]TDIssue, len(tdIssues))
	for _, td := range tdIssues {
		tdByID[td.ID] = td
	}

	for _, ext := range extIssues {
		tdID, entry := state.FindByExternalID(ext.ID)

		if entry != nil {
			// Already mapped — update if external changed since last sync
			if ext.UpdatedAt.After(entry.ExtUpdatedAt) {
				mapped := mapper.ExternalToTD(ext)
				// Include sync indicator label
				syncLbl := mapper.SyncLabel(ext.ID)
				if !containsLabel(mapped.Labels, syncLbl) {
					mapped.Labels = append(mapped.Labels, syncLbl)
				}
				if err := updateTDIssue(workDir, tdID, mapped); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("update td %s: %v", tdID, err))
					continue
				}
				// Update sync state timestamps
				state.Issues[tdID] = SyncStateEntry{
					ExternalID:   ext.ID,
					TDUpdatedAt:  time.Now(),
					ExtUpdatedAt: ext.UpdatedAt,
				}
				result.Pulled++
			}
		} else {
			// New issue — create in td
			mapped := mapper.ExternalToTD(ext)
			newID, err := createTDIssue(workDir, mapped)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("create td for %s %s: %v", provider.Name(), ext.ID, err))
				continue
			}

			// Set status (if not open) and sync label via update
			syncLbl := mapper.SyncLabel(ext.ID)
			update := TDIssue{Labels: append(mapped.Labels, syncLbl)}
			if mapped.Status != "" && mapped.Status != "open" {
				update.Status = mapped.Status
			}
			if err := updateTDIssue(workDir, newID, update); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("update td %s labels/status: %v", newID, err))
			}

			state.Issues[newID] = SyncStateEntry{
				ExternalID:   ext.ID,
				TDUpdatedAt:  time.Now(),
				ExtUpdatedAt: ext.UpdatedAt,
			}
			result.Pulled++
		}
	}

	// Save sync state
	if err := SaveState(todosDir, provider.ID(), state); err != nil {
		return result, fmt.Errorf("save sync state: %w", err)
	}

	return result, nil
}

// Push sends local td issues to the external provider.
func Push(ctx context.Context, provider Provider, workDir, todosDir string) (*SyncResult, error) {
	result := &SyncResult{}
	mapper := provider.Mapper()

	// Get all local td issues
	tdIssues, err := listTDIssues(workDir)
	if err != nil {
		return nil, fmt.Errorf("list td issues: %w", err)
	}

	// Load sync state
	state, err := LoadState(todosDir, provider.ID())
	if err != nil {
		return nil, fmt.Errorf("load sync state: %w", err)
	}

	for _, td := range tdIssues {
		entry := state.FindByTDID(td.ID)
		ext := mapper.TDToExternal(td)

		if entry != nil {
			// Already mapped — update if td changed since last sync
			if td.UpdatedAt.After(entry.TDUpdatedAt) {
				if err := provider.Update(ctx, workDir, entry.ExternalID, ext); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("update %s %s: %v", provider.Name(), entry.ExternalID, err))
					continue
				}
				// Handle state changes (close/reopen)
				if ext.State == "closed" {
					if err := provider.Close(ctx, workDir, entry.ExternalID); err != nil {
						result.Errors = append(result.Errors, fmt.Sprintf("close %s %s: %v", provider.Name(), entry.ExternalID, err))
					}
				} else {
					// Reopen if needed — reopen on already-open issue is a no-op
					if err := provider.Reopen(ctx, workDir, entry.ExternalID); err != nil {
						_ = err
					}
				}
				state.Issues[td.ID] = SyncStateEntry{
					ExternalID:   entry.ExternalID,
					TDUpdatedAt:  td.UpdatedAt,
					ExtUpdatedAt: time.Now(),
				}
				result.Pushed++
			}
		} else {
			// New issue — create on external provider
			newID, err := provider.Create(ctx, workDir, ext)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("create %s for %s: %v", provider.Name(), td.ID, err))
				continue
			}

			// If the issue is closed, close it on external too
			if ext.State == "closed" {
				if err := provider.Close(ctx, workDir, newID); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("close %s %s: %v", provider.Name(), newID, err))
				}
			}

			// Add sync label to td issue
			syncLbl := mapper.SyncLabel(newID)
			if !containsLabel(td.Labels, syncLbl) {
				if err := updateTDIssue(workDir, td.ID, TDIssue{Labels: append(td.Labels, syncLbl)}); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("update td %s labels: %v", td.ID, err))
				}
			}

			state.Issues[td.ID] = SyncStateEntry{
				ExternalID:   newID,
				TDUpdatedAt:  td.UpdatedAt,
				ExtUpdatedAt: time.Now(),
			}
			result.Pushed++
		}
	}

	// Save sync state
	if err := SaveState(todosDir, provider.ID(), state); err != nil {
		return result, fmt.Errorf("save sync state: %w", err)
	}

	return result, nil
}

// PushOne sends a single td issue to the external provider.
func PushOne(ctx context.Context, provider Provider, workDir, todosDir, tdIssueID string) (*SyncResult, error) {
	result := &SyncResult{}
	mapper := provider.Mapper()

	// Get the single td issue
	td, err := getTDIssue(workDir, tdIssueID)
	if err != nil {
		return nil, fmt.Errorf("get td issue %s: %w", tdIssueID, err)
	}

	// Load sync state
	state, err := LoadState(todosDir, provider.ID())
	if err != nil {
		return nil, fmt.Errorf("load sync state: %w", err)
	}

	entry := state.FindByTDID(td.ID)
	ext := mapper.TDToExternal(td)

	if entry != nil {
		// Already mapped — update on external
		if err := provider.Update(ctx, workDir, entry.ExternalID, ext); err != nil {
			return nil, fmt.Errorf("update %s %s: %w", provider.Name(), entry.ExternalID, err)
		}
		// Handle state changes (close/reopen)
		if ext.State == "closed" {
			if err := provider.Close(ctx, workDir, entry.ExternalID); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("close %s %s: %v", provider.Name(), entry.ExternalID, err))
			}
		} else {
			if err := provider.Reopen(ctx, workDir, entry.ExternalID); err != nil {
				_ = err
			}
		}
		state.Issues[td.ID] = SyncStateEntry{
			ExternalID:   entry.ExternalID,
			TDUpdatedAt:  td.UpdatedAt,
			ExtUpdatedAt: time.Now(),
		}
		result.Pushed++
	} else {
		// New issue — create on external
		newID, err := provider.Create(ctx, workDir, ext)
		if err != nil {
			return nil, fmt.Errorf("create %s for %s: %w", provider.Name(), td.ID, err)
		}

		if ext.State == "closed" {
			if err := provider.Close(ctx, workDir, newID); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("close %s %s: %v", provider.Name(), newID, err))
			}
		}

		// Add sync label to td issue
		syncLbl := mapper.SyncLabel(newID)
		if !containsLabel(td.Labels, syncLbl) {
			if err := updateTDIssue(workDir, td.ID, TDIssue{Labels: append(td.Labels, syncLbl)}); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("update td %s labels: %v", td.ID, err))
			}
		}

		state.Issues[td.ID] = SyncStateEntry{
			ExternalID:   newID,
			TDUpdatedAt:  td.UpdatedAt,
			ExtUpdatedAt: time.Now(),
		}
		result.Pushed++
	}

	// Save sync state
	if err := SaveState(todosDir, provider.ID(), state); err != nil {
		return result, fmt.Errorf("save sync state: %w", err)
	}

	return result, nil
}

// getTDIssue runs td show <id> -f json and returns a single parsed issue.
func getTDIssue(workDir, tdID string) (TDIssue, error) {
	cmd := exec.Command("td", "show", tdID, "-f", "json")
	cmd.Dir = workDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return TDIssue{}, fmt.Errorf("td show %s: %s", tdID, errMsg)
	}

	var issue TDIssue
	if err := json.Unmarshal(output, &issue); err != nil {
		return TDIssue{}, fmt.Errorf("parse td issue %s: %w", tdID, err)
	}

	return issue, nil
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
