#!/bin/bash
# Send arbitrary key sequences to a TUI app in tmux with proper timing
#
# Usage:
#   ./scripts/tui-send-keys.sh [options] <command> -- <key1> <key2> ...
#
# Options:
#   -s, --session NAME    Session name (default: tui-test)
#   -d, --delay MS        Delay between keys in ms (default: 500)
#   -w, --wait PATTERN    Wait for pattern before sending keys (default: none)
#   -t, --timeout SEC     Timeout waiting for app (default: 30)
#   -k, --keep            Keep session alive after keys sent
#   -c, --capture         Capture and print final pane state
#   -o, --output-dir DIR  Output directory for file captures (default: ./captures)
#
# Key syntax (tmux send-keys format):
#   Letters/numbers:  a, b, 1, 2, etc.
#   Enter:            Enter
#   Escape:           Escape
#   Tab:              Tab
#   Shift+Tab:        BTab
#   Arrow keys:       Up, Down, Left, Right
#   Ctrl+key:         C-c, C-v, C-x, etc.
#   Alt+key:          M-x, M-a, etc.
#   Function keys:    F1, F2, ... F12
#   Space:            Space
#   Backspace:        BSpace
#   Delete:           DC (or Delete)
#   Home/End:         Home, End
#   PageUp/Down:      PPage, NPage
#
# Inline actions (use between keys):
#   @capture              Capture and print current pane state to stdout
#   @capture:NAME.txt     Save as plain text with ANSI codes
#   @capture:NAME.html    Save as HTML (requires aha)
#   @capture:NAME.png     Save as PNG (requires termshot)
#   @capture:NAME         Save all available formats (txt + html if aha + png if termshot)
#   @sleep:MS             Sleep for MS milliseconds
#   @wait:PATTERN         Wait for PATTERN to appear in pane
#   @pause                Wait for user to press Enter (interactive)
#
# Examples:
#   # Open sidecar, navigate down, then quit
#   ./scripts/tui-send-keys.sh "go run ./cmd/sidecar" -w Sidecar -- Down Down Down Escape q y
#
#   # Capture state at different points (to stdout)
#   ./scripts/tui-send-keys.sh "go run ./cmd/sidecar" -w Sidecar -- \
#     @capture Down Down @capture Escape q @capture y
#
#   # Capture to files at each step
#   ./scripts/tui-send-keys.sh "go run ./cmd/sidecar" -w Sidecar -o ./test-output -- \
#     @capture:01-initial.png Down Down @capture:02-navigated @capture:03-quit.html y
#
#   # Open vim, type some text, save and quit
#   ./scripts/tui-send-keys.sh "vim /tmp/test.txt" -w "~" -- i "hello world" Escape ":wq" Enter
#
#   # Use shift+tab to navigate backwards
#   ./scripts/tui-send-keys.sh "./myapp" -w Ready -- Tab Tab BTab Enter
#
#   # Wait for specific UI states between actions
#   ./scripts/tui-send-keys.sh "./myapp" -- @wait:Ready Tab @wait:Menu Enter

set -e

# Defaults
SESSION="tui-test"
DELAY_MS=500
WAIT_PATTERN=""
TIMEOUT=30
KEEP=false
CAPTURE=false
OUTPUT_DIR="./captures"
COMMAND=""
KEYS=()

# Parse arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    -s|--session)
      SESSION="$2"
      shift 2
      ;;
    -d|--delay)
      DELAY_MS="$2"
      shift 2
      ;;
    -w|--wait)
      WAIT_PATTERN="$2"
      shift 2
      ;;
    -t|--timeout)
      TIMEOUT="$2"
      shift 2
      ;;
    -k|--keep)
      KEEP=true
      shift
      ;;
    -c|--capture)
      CAPTURE=true
      shift
      ;;
    -o|--output-dir)
      OUTPUT_DIR="$2"
      shift 2
      ;;
    --)
      shift
      KEYS=("$@")
      break
      ;;
    -h|--help)
      head -61 "$0" | tail -60
      exit 0
      ;;
    *)
      if [[ -z "$COMMAND" ]]; then
        COMMAND="$1"
      else
        echo "Error: Unexpected argument: $1" >&2
        exit 1
      fi
      shift
      ;;
  esac
done

if [[ -z "$COMMAND" ]]; then
  echo "Error: No command specified" >&2
  echo "Usage: $0 [options] <command> -- <key1> <key2> ..." >&2
  exit 1
fi

# Convert ms to seconds for sleep
DELAY_SEC=$(echo "scale=3; $DELAY_MS / 1000" | bc)

# Capture helper function for file output
capture_to_file() {
  local name="$1"
  local format="$2"  # txt, html, png, or "all"

  mkdir -p "$OUTPUT_DIR"
  local txt_file="$OUTPUT_DIR/$name.txt"

  # Always capture ANSI text first (needed for all formats)
  tmux capture-pane -t "$SESSION" -e -p > "$txt_file"

  case "$format" in
    txt)
      echo "Saved: $txt_file"
      ;;
    html)
      if command -v aha &>/dev/null; then
        cat "$txt_file" | aha --black > "$OUTPUT_DIR/$name.html"
        echo "Saved: $OUTPUT_DIR/$name.html"
      else
        echo "Warning: aha not installed, skipping HTML"
      fi
      rm -f "$txt_file"
      ;;
    png)
      if command -v termshot &>/dev/null; then
        local cols
        cols=$(tmux display-message -t "$SESSION" -p '#{pane_width}')
        termshot --raw-read "$txt_file" --columns "$cols" --filename "$OUTPUT_DIR/$name.png" 2>/dev/null
        echo "Saved: $OUTPUT_DIR/$name.png"
      else
        echo "Warning: termshot not installed, skipping PNG"
      fi
      rm -f "$txt_file"
      ;;
    all)
      # Keep txt
      echo "Saved: $txt_file"
      # Add html if aha available
      if command -v aha &>/dev/null; then
        cat "$txt_file" | aha --black > "$OUTPUT_DIR/$name.html"
        echo "Saved: $OUTPUT_DIR/$name.html"
      fi
      # Add png if termshot available
      if command -v termshot &>/dev/null; then
        local cols
        cols=$(tmux display-message -t "$SESSION" -p '#{pane_width}')
        termshot --raw-read "$txt_file" --columns "$cols" --filename "$OUTPUT_DIR/$name.png" 2>/dev/null
        echo "Saved: $OUTPUT_DIR/$name.png"
      fi
      ;;
  esac
}

# Get current terminal dimensions for proper screenshot sizing
TERM_COLS=$(tput cols 2>/dev/null || echo 80)
TERM_LINES=$(tput lines 2>/dev/null || echo 24)

# Kill any existing session with same name
tmux kill-session -t "$SESSION" 2>/dev/null || true

# Start tmux session with the command (using current terminal size)
tmux new-session -d -s "$SESSION" -x "$TERM_COLS" -y "$TERM_LINES" "$COMMAND"

# Wait for pattern if specified
if [[ -n "$WAIT_PATTERN" ]]; then
  echo "Waiting for '$WAIT_PATTERN'..."
  for ((i=0; i<TIMEOUT*2; i++)); do
    if tmux capture-pane -t "$SESSION" -p 2>/dev/null | grep -q "$WAIT_PATTERN"; then
      echo "Pattern found, sending keys..."
      sleep "$DELAY_SEC"
      break
    fi
    sleep 0.5
  done

  if ! tmux capture-pane -t "$SESSION" -p 2>/dev/null | grep -q "$WAIT_PATTERN"; then
    echo "Error: Timeout waiting for pattern '$WAIT_PATTERN'" >&2
    tmux kill-session -t "$SESSION" 2>/dev/null || true
    exit 1
  fi
fi

# Send each key with delay, handling special @ commands
CAPTURE_COUNT=0
for key in "${KEYS[@]}"; do
  case "$key" in
    @capture:*)
      # Capture to file(s)
      spec="${key#@capture:}"
      cap_name="${spec%.*}"
      cap_ext="${spec##*.}"

      if [[ "$cap_name" == "$cap_ext" ]]; then
        # No extension: @capture:NAME → save all formats
        capture_to_file "$cap_name" "all"
      else
        # Has extension: @capture:NAME.png → save specific format
        capture_to_file "$cap_name" "$cap_ext"
      fi
      ;;
    @capture)
      # Capture current pane state to stdout
      ((CAPTURE_COUNT++))
      echo "=== Capture #$CAPTURE_COUNT ==="
      tmux capture-pane -t "$SESSION" -p 2>/dev/null || echo "(session ended)"
      echo "========================"
      ;;
    @sleep:*)
      # Sleep for specified milliseconds
      MS="${key#@sleep:}"
      SLEEP_SEC=$(echo "scale=3; $MS / 1000" | bc)
      echo "Sleeping ${MS}ms..."
      sleep "$SLEEP_SEC"
      ;;
    @wait:*)
      # Wait for pattern
      PATTERN="${key#@wait:}"
      echo "Waiting for '$PATTERN'..."
      for ((i=0; i<TIMEOUT*2; i++)); do
        if tmux capture-pane -t "$SESSION" -p 2>/dev/null | grep -q "$PATTERN"; then
          break
        fi
        sleep 0.5
      done
      ;;
    @pause)
      # Wait for user input (interactive debugging)
      echo "Paused. Press Enter to continue..."
      read -r
      ;;
    *)
      # Regular key - send to tmux
      tmux send-keys -t "$SESSION" "$key"
      sleep "$DELAY_SEC"
      ;;
  esac
done

# Capture final state if requested
if [[ "$CAPTURE" == true ]]; then
  sleep "$DELAY_SEC"
  echo "=== Final pane state ==="
  tmux capture-pane -t "$SESSION" -p 2>/dev/null || echo "(session ended)"
  echo "========================"
fi

# Clean up or keep session
if [[ "$KEEP" == true ]]; then
  echo "Session '$SESSION' kept alive. Attach with: tmux attach -t $SESSION"
else
  sleep "$DELAY_SEC"
  tmux kill-session -t "$SESSION" 2>/dev/null || true
  echo "Done"
fi
