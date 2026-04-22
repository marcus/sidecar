package jj

import (
	"errors"
	"strings"
	"testing"
)

type fakeExec struct {
	run func(dir string, name string, args ...string) (string, string, error)
}

func (f fakeExec) Run(dir string, name string, args ...string) (string, string, error) {
	return f.run(dir, name, args...)
}

func TestDetectSuccess(t *testing.T) {
	c := NewWithExecutor(fakeExec{
		run: func(dir string, name string, args ...string) (string, string, error) {
			return "/tmp/repo\n", "", nil
		},
	})

	root, ok, err := c.Detect("/tmp/repo")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if root != "/tmp/repo" {
		t.Fatalf("unexpected root: %q", root)
	}
}

func TestDetectNotWorkspace(t *testing.T) {
	c := NewWithExecutor(fakeExec{
		run: func(dir string, name string, args ...string) (string, string, error) {
			return "", "not a jj workspace", errors.New("exit status 1")
		},
	})

	_, ok, err := c.Detect("/tmp/repo")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false")
	}
}

func TestStatusCommandError(t *testing.T) {
	c := NewWithExecutor(fakeExec{
		run: func(dir string, name string, args ...string) (string, string, error) {
			return "", "boom", errors.New("exit status 1")
		},
	})

	_, err := c.Status("/tmp/repo")
	if err == nil {
		t.Fatal("expected error")
	}
	var cmdErr *CommandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("expected *CommandError, got %T", err)
	}
}

func TestLogParsesCommits(t *testing.T) {
	c := NewWithExecutor(fakeExec{
		run: func(dir string, name string, args ...string) (string, string, error) {
			out := "abc12345\x1fdeadbeef\x1ffirst\nffff0000\x1f11112222\x1fsecond\n"
			return out, "", nil
		},
	})

	commits, err := c.Log("/tmp/repo", LogOptions{Limit: 2})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}
	if commits[0].ChangeID != "abc12345" || commits[0].CommitID != "deadbeef" || commits[0].Subject != "first" {
		t.Fatalf("unexpected first commit: %+v", commits[0])
	}
}

func TestLogPassesOptions(t *testing.T) {
	var gotArgs []string
	c := NewWithExecutor(fakeExec{
		run: func(dir string, name string, args ...string) (string, string, error) {
			gotArgs = append([]string{}, args...)
			return "", "", nil
		},
	})

	_, err := c.Log("/tmp/repo", LogOptions{
		Revisions: "::@",
		Limit:     5,
		Paths:     []string{"internal/vcs/jj/client.go"},
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	got := strings.Join(gotArgs, " ")
	for _, expected := range []string{
		"--no-pager",
		"log",
		"--color=never",
		"--no-graph",
		"-r ::@",
		"--limit 5",
		"--template",
		"internal/vcs/jj/client.go",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("expected args to contain %q; got: %q", expected, got)
		}
	}
}

func TestDiffPassesOptions(t *testing.T) {
	var gotArgs []string
	c := NewWithExecutor(fakeExec{
		run: func(dir string, name string, args ...string) (string, string, error) {
			gotArgs = append([]string{}, args...)
			return "diff-body", "", nil
		},
	})

	out, err := c.Diff("/tmp/repo", DiffOptions{
		Revision: "@-",
		Paths:    []string{"internal/vcs/jj/client.go"},
		Git:      true,
		Context:  7,
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out != "diff-body" {
		t.Fatalf("unexpected output: %q", out)
	}
	got := strings.Join(gotArgs, " ")
	for _, expected := range []string{
		"--no-pager",
		"diff",
		"--color=never",
		"-r @-",
		"--git",
		"--context 7",
		"internal/vcs/jj/client.go",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("expected args to contain %q; got: %q", expected, got)
		}
	}
}

func TestLogGraphParsesCommits(t *testing.T) {
	c := NewWithExecutor(fakeExec{
		run: func(dir string, name string, args ...string) (string, string, error) {
			return "@  user date abcdef12\n│  msg\n", "", nil
		},
	})

	graph, err := c.LogGraph("/tmp/repo", 10)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(graph.Commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(graph.Commits))
	}
	if graph.Commits[0].CommitID != "abcdef12" {
		t.Fatalf("unexpected commit id: %+v", graph.Commits[0])
	}
}

func TestLogStructuredParsesCommits(t *testing.T) {
	c := NewWithExecutor(fakeExec{
		run: func(dir string, name string, args ...string) (string, string, error) {
			out := "pnrtzttk\x1fcb5b94f6c470\x1fdocs: design\x1fuser@test.com\x1f4 days ago\x1fmain\x1f\x1f\x1f\x1fwc\x1f8413c957dd52\x1fp\n"
			return out, "", nil
		},
	})

	commits, err := c.LogStructured("/tmp/repo", 10)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(commits))
	}
	if commits[0].ChangeID != "pnrtzttk" {
		t.Fatalf("unexpected ChangeID: %q", commits[0].ChangeID)
	}
	if !commits[0].IsWorkingCopy {
		t.Fatal("expected IsWorkingCopy")
	}
}

func TestLogStructuredPassesArgs(t *testing.T) {
	var gotArgs []string
	c := NewWithExecutor(fakeExec{
		run: func(dir string, name string, args ...string) (string, string, error) {
			gotArgs = append([]string{}, args...)
			return "", "", nil
		},
	})

	_, _ = c.LogStructured("/tmp/repo", 20)
	got := strings.Join(gotArgs, " ")
	for _, expected := range []string{"--no-pager", "log", "--no-graph", "--color=never", "--limit 20", "--template"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("expected args to contain %q; got: %q", expected, got)
		}
	}
}

func TestEditPassesCommit(t *testing.T) {
	var gotArgs []string
	c := NewWithExecutor(fakeExec{
		run: func(dir string, name string, args ...string) (string, string, error) {
			gotArgs = append([]string{}, args...)
			return "", "", nil
		},
	})

	if err := c.Edit("/tmp/repo", "abc12345"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	got := strings.Join(gotArgs, " ")
	if !strings.Contains(got, "--no-pager edit abc12345") {
		t.Fatalf("unexpected args: %q", got)
	}
}
