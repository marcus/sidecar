package jj

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Executor runs external commands and returns stdout/stderr.
type Executor interface {
	Run(dir string, name string, args ...string) (stdout string, stderr string, err error)
}

type osExec struct{}

func (osExec) Run(dir string, name string, args ...string) (string, string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// CommandError wraps an execution failure with command details.
type CommandError struct {
	Command string
	Stderr  string
	Err     error
}

func (e *CommandError) Error() string {
	if e.Stderr == "" {
		return fmt.Sprintf("%s: %v", e.Command, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Command, e.Stderr)
}

// Client provides typed operations backed by the jj CLI.
type Client struct {
	exec Executor
}

const logLineTemplate = "change_id.short(8) ++ \"\\x1f\" ++ commit_id.short(8) ++ \"\\x1f\" ++ description.first_line() ++ \"\\n\""

// New returns a client that uses the system jj binary.
func New() *Client {
	return &Client{exec: osExec{}}
}

// NewWithExecutor returns a client with a custom executor (for tests).
func NewWithExecutor(ex Executor) *Client {
	return &Client{exec: ex}
}

// Detect checks if workDir is inside a jj workspace and returns workspace root.
func (c *Client) Detect(workDir string) (root string, ok bool, err error) {
	stdout, stderr, runErr := c.runJJ(workDir, "root")
	if runErr != nil {
		if strings.TrimSpace(stderr) == "" {
			return "", false, nil
		}
		return "", false, nil
	}
	root = strings.TrimSpace(stdout)
	if root == "" {
		return "", false, nil
	}
	return root, true, nil
}

// Status returns a parsed working-copy status snapshot.
func (c *Client) Status(workDir string) (StatusSnapshot, error) {
	stdout, stderr, runErr := c.runJJ(workDir, "status", "--color=never")
	if runErr != nil {
		return StatusSnapshot{}, &CommandError{
			Command: "jj --no-pager status --color=never",
			Stderr:  strings.TrimSpace(stderr),
			Err:     runErr,
		}
	}
	return ParseStatusOutput(stdout), nil
}

// Log returns parsed commit summaries for the requested revision set.
func (c *Client) Log(workDir string, opts LogOptions) ([]Commit, error) {
	args := []string{"log", "--color=never", "--no-graph"}
	if opts.Revisions != "" {
		args = append(args, "-r", opts.Revisions)
	}
	if opts.Limit > 0 {
		args = append(args, "--limit", strconv.Itoa(opts.Limit))
	}
	args = append(args, "--template", logLineTemplate)
	if len(opts.Paths) > 0 {
		args = append(args, opts.Paths...)
	}

	stdout, stderr, runErr := c.runJJ(workDir, args...)
	if runErr != nil {
		return nil, &CommandError{
			Command: "jj --no-pager " + strings.Join(args, " "),
			Stderr:  strings.TrimSpace(stderr),
			Err:     runErr,
		}
	}
	return ParseLogOutput(stdout), nil
}

// Diff returns the textual diff for the requested target.
func (c *Client) Diff(workDir string, opts DiffOptions) (string, error) {
	args := []string{"diff", "--color=never"}
	if opts.Revision != "" {
		args = append(args, "-r", opts.Revision)
	}
	if opts.Git {
		args = append(args, "--git")
	}
	if opts.Summary {
		args = append(args, "--summary")
	}
	if opts.NameOnly {
		args = append(args, "--name-only")
	}
	if opts.Context > 0 {
		args = append(args, "--context", strconv.Itoa(opts.Context))
	}
	if len(opts.Paths) > 0 {
		args = append(args, opts.Paths...)
	}

	stdout, stderr, runErr := c.runJJ(workDir, args...)
	if runErr != nil {
		return "", &CommandError{
			Command: "jj --no-pager " + strings.Join(args, " "),
			Stderr:  strings.TrimSpace(stderr),
			Err:     runErr,
		}
	}
	return stdout, nil
}

// LogGraph returns default graph-rendered log output with commit anchors.
func (c *Client) LogGraph(workDir string, limit int) (LogGraph, error) {
	args := []string{"log", "--color=always"}
	if limit > 0 {
		args = append(args, "--limit", strconv.Itoa(limit))
	}
	stdout, stderr, runErr := c.runJJ(workDir, args...)
	if runErr != nil {
		return LogGraph{}, &CommandError{
			Command: "jj --no-pager " + strings.Join(args, " "),
			Stderr:  strings.TrimSpace(stderr),
			Err:     runErr,
		}
	}
	return ParseLogGraphOutput(stdout), nil
}

const structuredLogTemplate = `change_id.short(8) ++ "\x1f" ++ commit_id.short(12) ++ "\x1f" ++ description.first_line() ++ "\x1f" ++ author.email() ++ "\x1f" ++ author.timestamp().ago() ++ "\x1f" ++ bookmarks ++ "\x1f" ++ if(empty, "empty", "") ++ "\x1f" ++ if(conflict, "conflict", "") ++ "\x1f" ++ if(immutable, "immutable", "") ++ "\x1f" ++ if(working_copies, "wc", "") ++ "\x1f" ++ parents.map(|p| p.commit_id().short(12)).join(",") ++ "\x1f" ++ change_id.shortest(1) ++ "\n"`

// LogStructured returns fully parsed commit metadata for custom rendering.
func (c *Client) LogStructured(workDir string, limit int) ([]StructuredCommit, error) {
	args := []string{"log", "--no-graph", "--color=never"}
	if limit > 0 {
		args = append(args, "--limit", strconv.Itoa(limit))
	}
	args = append(args, "--template", structuredLogTemplate)

	stdout, stderr, runErr := c.runJJ(workDir, args...)
	if runErr != nil {
		return nil, &CommandError{
			Command: "jj --no-pager " + strings.Join(args, " "),
			Stderr:  strings.TrimSpace(stderr),
			Err:     runErr,
		}
	}
	return ParseStructuredLogOutput(stdout), nil
}

// Edit sets the working copy to edit the given commit.
func (c *Client) Edit(workDir string, commitID string) error {
	args := []string{"edit", commitID}
	_, stderr, runErr := c.runJJ(workDir, args...)
	if runErr != nil {
		return &CommandError{
			Command: "jj --no-pager " + strings.Join(args, " "),
			Stderr:  strings.TrimSpace(stderr),
			Err:     runErr,
		}
	}
	return nil
}

func (c *Client) runJJ(workDir string, args ...string) (stdout string, stderr string, err error) {
	fullArgs := make([]string, 0, len(args)+1)
	fullArgs = append(fullArgs, "--no-pager")
	fullArgs = append(fullArgs, args...)
	return c.exec.Run(workDir, "jj", fullArgs...)
}
