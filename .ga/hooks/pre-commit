#!/bin/sh
# conventional commit hook - validates commit message format
# receives JSON payload on stdin: {"commit_message": "...", ...}

# --- extract commit_message from JSON payload ---
if command -v python3 >/dev/null 2>&1; then
  MSG=$(python3 -c 'import sys,json; print(json.load(sys.stdin).get("commit_message",""))')
elif command -v jq >/dev/null 2>&1; then
  MSG=$(jq -r '.commit_message')
else
  echo "ga: pre-commit hook requires python3 or jq" >&2
  exit 1
fi

if [ -z "$MSG" ]; then
  echo "ga: failed to extract commit_message from payload" >&2
  exit 1
fi

# --- validate header (first line) ---
HEADER=$(printf '%s' "$MSG" | head -n1)

if ! printf '%s' "$HEADER" | grep -qE \
  '^(feat|fix|docs|style|refactor|perf|test|chore|build|ci|revert)(\([a-zA-Z0-9._/ -]+\))?!?: .+'; then
  echo "ga: commit message does not follow Conventional Commits format" >&2
  echo "ga: expected: <type>[optional scope][!]: <description>" >&2
  exit 1
fi

# --- validate blank line after header (if body exists) ---
LINE_COUNT=$(printf '%s' "$MSG" | wc -l | tr -d ' ')
if [ "$LINE_COUNT" -ge 1 ]; then
  SECOND_LINE=$(printf '%s' "$MSG" | sed -n '2p')
  if [ -n "$SECOND_LINE" ]; then
    echo "ga: body must be separated from header by a blank line" >&2
    exit 1
  fi
fi

exit 0
