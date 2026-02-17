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

// GitHubProvider implements Provider for GitHub Issues using the gh CLI.
type GitHubProvider struct{}

// ghIssue is the JSON structure returned by gh issue list.
type ghIssue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	Labels    []ghLabel `json:"labels"`
	UpdatedAt time.Time `json:"updatedAt"`
	CreatedAt time.Time `json:"createdAt"`
}

type ghLabel struct {
	Name string `json:"name"`
}

// NewGitHubProvider creates a new GitHub Issues provider.
func NewGitHubProvider() *GitHubProvider {
	return &GitHubProvider{}
}

func (g *GitHubProvider) ID() string     { return "github" }
func (g *GitHubProvider) Name() string   { return "GitHub" }
func (g *GitHubProvider) Mapper() Mapper { return &GitHubMapper{} }

// Available checks if gh is installed and the workDir is a GitHub repo.
func (g *GitHubProvider) Available(workDir string) (bool, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return false, fmt.Errorf("gh CLI not found: install from https://cli.github.com")
	}

	// Check that the remote is a GitHub repo
	remoteURL := getRemoteURL(workDir)
	if remoteURL == "" {
		return false, fmt.Errorf("no git remote 'origin' configured")
	}
	if !strings.Contains(remoteURL, "github.com") {
		return false, fmt.Errorf("remote is not a GitHub repository")
	}

	return true, nil
}

// List returns all issues from the GitHub repo.
func (g *GitHubProvider) List(ctx context.Context, workDir string) ([]ExternalIssue, error) {
	cmd := exec.CommandContext(ctx, "gh", "issue", "list",
		"--json", "number,title,body,state,labels,updatedAt,createdAt",
		"--limit", "200",
		"--state", "all",
	)
	cmd.Dir = workDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return nil, fmt.Errorf("gh issue list: %s", errMsg)
	}

	var issues []ghIssue
	if err := json.Unmarshal(output, &issues); err != nil {
		return nil, fmt.Errorf("parse gh issues: %w", err)
	}

	result := make([]ExternalIssue, len(issues))
	for i, issue := range issues {
		labels := make([]string, len(issue.Labels))
		for j, l := range issue.Labels {
			labels[j] = l.Name
		}
		result[i] = ExternalIssue{
			ID:        strconv.Itoa(issue.Number),
			Title:     issue.Title,
			Body:      issue.Body,
			State:     strings.ToLower(issue.State),
			Labels:    labels,
			CreatedAt: issue.CreatedAt,
			UpdatedAt: issue.UpdatedAt,
		}
	}

	return result, nil
}

// Create creates a new GitHub issue and returns its number as a string.
// Labels are only set if they already exist on the GitHub repo.
func (g *GitHubProvider) Create(ctx context.Context, workDir string, issue ExternalIssue) (string, error) {
	args := []string{"issue", "create",
		"--title", issue.Title,
		"--body", issue.Body,
	}
	if len(issue.Labels) > 0 {
		repoLabels, err := listRepoLabels(ctx, workDir)
		if err == nil {
			for _, label := range issue.Labels {
				if repoLabels[label] {
					args = append(args, "--label", label)
				}
			}
		}
	}

	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = workDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return "", fmt.Errorf("gh issue create: %s", errMsg)
	}

	// gh issue create outputs the issue URL; extract the number from it
	url := strings.TrimSpace(string(output))
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1], nil
	}

	return "", fmt.Errorf("unexpected output from gh issue create: %s", url)
}

// Update updates an existing GitHub issue.
// Labels are only added if they already exist on the GitHub repo.
func (g *GitHubProvider) Update(ctx context.Context, workDir string, id string, issue ExternalIssue) error {
	args := []string{"issue", "edit", id,
		"--title", issue.Title,
		"--body", issue.Body,
	}

	// Set labels (--add-label for each label), only if they exist on the repo
	if len(issue.Labels) > 0 {
		repoLabels, err := listRepoLabels(ctx, workDir)
		if err == nil {
			for _, label := range issue.Labels {
				if repoLabels[label] {
					args = append(args, "--add-label", label)
				}
			}
		}
	}

	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = workDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return fmt.Errorf("gh issue edit %s: %s", id, errMsg)
	}

	return nil
}

// Close closes a GitHub issue.
func (g *GitHubProvider) Close(ctx context.Context, workDir string, id string) error {
	cmd := exec.CommandContext(ctx, "gh", "issue", "close", id)
	cmd.Dir = workDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return fmt.Errorf("gh issue close %s: %s", id, errMsg)
	}

	return nil
}

// Reopen reopens a GitHub issue.
func (g *GitHubProvider) Reopen(ctx context.Context, workDir string, id string) error {
	cmd := exec.CommandContext(ctx, "gh", "issue", "reopen", id)
	cmd.Dir = workDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return fmt.Errorf("gh issue reopen %s: %s", id, errMsg)
	}

	return nil
}

// listRepoLabels returns a set of label names that exist on the GitHub repo.
func listRepoLabels(ctx context.Context, workDir string) (map[string]bool, error) {
	cmd := exec.CommandContext(ctx, "gh", "label", "list", "--json", "name", "--limit", "200")
	cmd.Dir = workDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return nil, fmt.Errorf("gh label list: %s", errMsg)
	}

	var labels []ghLabel
	if err := json.Unmarshal(output, &labels); err != nil {
		return nil, fmt.Errorf("parse gh labels: %w", err)
	}

	result := make(map[string]bool, len(labels))
	for _, l := range labels {
		result[l.Name] = true
	}
	return result, nil
}

// getRemoteURL returns the URL for the primary remote (origin).
func getRemoteURL(workDir string) string {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}
