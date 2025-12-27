package conversations

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/sst/sidecar/internal/adapter"
)

// ExportSessionAsMarkdown converts a session and its messages to markdown format.
func ExportSessionAsMarkdown(session *adapter.Session, messages []adapter.Message) string {
	var sb strings.Builder

	// Header
	sessionName := "Unknown Session"
	if session != nil && session.Name != "" {
		sessionName = session.Name
	} else if session != nil {
		sessionName = session.ID
	}

	sb.WriteString(fmt.Sprintf("# Session: %s\n\n", sessionName))

	if session != nil {
		sb.WriteString(fmt.Sprintf("**Date**: %s\n", session.CreatedAt.Format("2006-01-02 15:04")))
		if session.Duration > 0 {
			sb.WriteString(fmt.Sprintf("**Duration**: %s\n", formatExportDuration(session.Duration)))
		}
		if session.TotalTokens > 0 {
			sb.WriteString(fmt.Sprintf("**Tokens**: %d\n", session.TotalTokens))
		}
		if session.EstCost > 0 {
			sb.WriteString(fmt.Sprintf("**Estimated Cost**: $%.2f\n", session.EstCost))
		}
		sb.WriteString("\n---\n\n")
	}

	// Messages
	for _, msg := range messages {
		// Role and timestamp
		role := strings.Title(msg.Role)
		ts := msg.Timestamp.Format("15:04:05")
		sb.WriteString(fmt.Sprintf("## %s (%s)\n\n", role, ts))

		// Model info for assistant messages
		if msg.Role == "assistant" && msg.Model != "" {
			sb.WriteString(fmt.Sprintf("*Model: %s*\n\n", modelShortName(msg.Model)))
		}

		// Token info
		if msg.InputTokens > 0 || msg.OutputTokens > 0 {
			sb.WriteString(fmt.Sprintf("*Tokens: in=%d, out=%d*\n\n", msg.InputTokens, msg.OutputTokens))
		}

		// Thinking blocks (if any)
		if len(msg.ThinkingBlocks) > 0 {
			for _, tb := range msg.ThinkingBlocks {
				sb.WriteString("<details>\n")
				sb.WriteString(fmt.Sprintf("<summary>Thinking (%d tokens)</summary>\n\n", tb.TokenCount))
				sb.WriteString(tb.Content)
				sb.WriteString("\n\n</details>\n\n")
			}
		}

		// Content
		sb.WriteString(msg.Content)
		sb.WriteString("\n\n")

		// Tool uses
		if len(msg.ToolUses) > 0 {
			sb.WriteString("**Tools used:**\n")
			for _, tool := range msg.ToolUses {
				filePath := extractFilePath(tool.Input)
				if filePath != "" {
					sb.WriteString(fmt.Sprintf("- %s: `%s`\n", tool.Name, filePath))
				} else {
					sb.WriteString(fmt.Sprintf("- %s\n", tool.Name))
				}
			}
			sb.WriteString("\n")
		}

		sb.WriteString("---\n\n")
	}

	return sb.String()
}

// CopyToClipboard copies content to the system clipboard.
func CopyToClipboard(content string) error {
	return clipboard.WriteAll(content)
}

// ExportSessionToFile writes a session to a markdown file.
func ExportSessionToFile(session *adapter.Session, messages []adapter.Message, workDir string) (string, error) {
	md := ExportSessionAsMarkdown(session, messages)

	// Generate filename from session name or ID
	name := "session"
	if session != nil && session.Name != "" {
		name = sanitizeFilename(session.Name)
	} else if session != nil {
		name = session.ID[:8]
	}

	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("%s-%s.md", name, timestamp)
	path := filepath.Join(workDir, filename)

	if err := os.WriteFile(path, []byte(md), 0644); err != nil {
		return "", err
	}

	return filename, nil
}

// formatExportDuration formats duration for export.
func formatExportDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

// sanitizeFilename removes or replaces characters that are invalid in filenames.
func sanitizeFilename(name string) string {
	// Replace problematic characters
	replacer := strings.NewReplacer(
		"/", "-",
		"\\", "-",
		":", "-",
		"*", "",
		"?", "",
		"\"", "",
		"<", "",
		">", "",
		"|", "",
		"\n", " ",
		"\r", "",
	)
	name = replacer.Replace(name)

	// Truncate if too long
	if len(name) > 50 {
		name = name[:50]
	}

	// Trim spaces and dashes from ends
	name = strings.Trim(name, " -")

	if name == "" {
		name = "session"
	}

	return name
}
