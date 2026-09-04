package cli

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

// The Devin payload suite.
//
// Devin is the first provider whose hook payload is not uniformly snake_case,
// and the failure a wrong reading produces is silent: a payload naming no
// session records nothing by design, so an extraction that could not see
// `sessionId` would leave every Devin pane unbound with no error anywhere. The
// fixture is what makes each branch visible, and it is checked in rather than
// written inline so a reviewer can diff it against the payload shapes Herdr's
// own devin asset reads.

// devinPayloadRow is one row of testdata/devin/payloads.tsv.
type devinPayloadRow struct {
	name    string
	payload string
	outcome string
	want    string
}

func readDevinPayloadRows(t *testing.T) []devinPayloadRow {
	t.Helper()
	f, err := os.Open("testdata/devin/payloads.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	var rows []devinPayloadRow
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scan.Scan() {
		line := scan.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 4 {
			t.Fatalf("payloads.tsv row %q has %d columns, want 4", line, len(fields))
		}
		rows = append(rows, devinPayloadRow{name: fields[0], payload: fields[1], outcome: fields[2], want: fields[3]})
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("payloads.tsv holds no rows")
	}
	return rows
}

// TestDevinPayloadsResolveToTheReferenceTheFixtureRecords drives the real
// extraction over every recorded payload.
//
// It reads through readHookPayload and the hookPayload accessors -- sessionID,
// transcriptPath and subAgentID, the same three the CLI itself calls -- rather
// than re-deriving the answer, so a change to any of them that broke a branch
// fails here against the fixture rather than passing against a copy of itself.
func TestDevinPayloadsResolveToTheReferenceTheFixtureRecords(t *testing.T) {
	seen := map[string]bool{}
	for _, row := range readDevinPayloadRows(t) {
		seen[row.outcome] = true
		t.Run(row.name, func(t *testing.T) {
			payload, err := readHookPayload(strings.NewReader(row.payload))
			if row.outcome == "invalid" {
				if err == nil {
					t.Fatalf("the payload decoded cleanly; the fixture records it as undecodable")
				}
				return
			}
			if err != nil {
				t.Fatalf("readHookPayload: %v", err)
			}
			switch row.outcome {
			case "subagent":
				if payload.subAgentID() == "" {
					t.Fatal("the fixture records a sub-agent payload but no agent id survived decoding")
				}
			case "id":
				if got := payload.subAgentID(); got != "" {
					t.Fatalf("agent id %q would make this a sub-agent payload", got)
				}
				if got := payload.sessionID(); got != row.want {
					t.Fatalf("sessionID() = %q, the fixture records %q", got, row.want)
				}
			case "path":
				if got := payload.sessionID(); got != "" {
					t.Fatalf("sessionID() = %q on a payload the fixture records as path-only", got)
				}
				if got := payload.transcriptPath(); got != row.want {
					t.Fatalf("transcriptPath() = %q, the fixture records %q", got, row.want)
				}
			case "none":
				if got := payload.sessionID(); got != "" {
					t.Fatalf("sessionID() = %q on a payload the fixture records as naming no session", got)
				}
				if got := payload.transcriptPath(); got != "" {
					t.Fatalf("transcriptPath() = %q on a payload the fixture records as naming no session", got)
				}
			default:
				t.Fatalf("unknown outcome %q", row.outcome)
			}
		})
	}
	// A fixture exercising a subset and calling it the ladder is the failure
	// this guards: every outcome the file documents has to appear in it.
	for _, outcome := range []string{"id", "path", "none", "subagent", "invalid"} {
		if !seen[outcome] {
			t.Errorf("no fixture row produces outcome %q", outcome)
		}
	}
}

// TestSnakeCaseWinsWhenAPayloadCarriesBothSpellings pins the precedence rather
// than leaving it to whichever field the struct happens to list first.
//
// Herdr reads ("session_id", "sessionId") in that order and takes the first
// non-empty string. If the two ever disagree on a released Devin, the two
// implementations have to pick the same one or a pane bound by Sidecar and a
// pane bound by Herdr would resume different conversations.
func TestSnakeCaseWinsWhenAPayloadCarriesBothSpellings(t *testing.T) {
	payload, err := readHookPayload(strings.NewReader(`{"session_id":"snake","sessionId":"camel"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := payload.sessionID(); got != "snake" {
		t.Fatalf("sessionID() = %q, want the snake_case value upstream reads first", got)
	}
	// And the camelCase value is used when snake_case is present but empty,
	// which is the same rule stated from the other side.
	payload, err = readHookPayload(strings.NewReader(`{"session_id":"","sessionId":"camel"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := payload.sessionID(); got != "camel" {
		t.Fatalf("sessionID() = %q, want the camelCase value", got)
	}
}

// TestTheCamelCaseSpellingChangesNothingForEveryOtherProvider is the blast
// radius. Every provider but Devin sends snake_case only, so adding a second
// field must not change what any of their payloads resolve to.
func TestTheCamelCaseSpellingChangesNothingForEveryOtherProvider(t *testing.T) {
	for _, payload := range []string{
		`{"hook_event_name":"SessionStart","session_id":"019f2c8a","transcript_path":"/tmp/t.jsonl"}`,
		`{"hook_event_name":"SessionStart","transcript_path":"/tmp/t.jsonl"}`,
		`{"hook_event_name":"SessionStart"}`,
	} {
		got, err := readHookPayload(strings.NewReader(payload))
		if err != nil {
			t.Fatalf("%s: %v", payload, err)
		}
		if got.sessionID() != strings.TrimSpace(got.SessionID) {
			t.Fatalf("%s: sessionID() = %q but session_id is %q", payload, got.sessionID(), got.SessionID)
		}
	}
}
