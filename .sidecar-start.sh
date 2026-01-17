#!/bin/bash
PROMPT=$(cat "/Users/marcusvorwaller/code/sidecar-p2-stories/.sidecar-prompt")
rm -f "/Users/marcusvorwaller/code/sidecar-p2-stories/.sidecar-prompt"
claude --dangerously-skip-permissions "$PROMPT"
rm -f "/Users/marcusvorwaller/code/sidecar-p2-stories/.sidecar-start.sh"
