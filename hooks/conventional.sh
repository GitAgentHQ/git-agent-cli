#!/bin/sh
# conventional commit hook - validates commit message format
# receives JSON payload on stdin: {"commit_message": "...", ...}

# --- extract commit_message from JSON payload ---
if command -v python3 >/dev/null 2>&1; then
  MSG=$(python3 -c 'import sys,json; print(json.load(sys.stdin).get("commit_message",""))')
elif command -v jq >/dev/null 2>&1; then
  MSG=$(jq -r '.commit_message')
else
  echo "git-agent: pre-commit hook requires python3 or jq" >&2
  exit 1
fi

if [ -z "$MSG" ]; then
  echo "git-agent: failed to extract commit_message from payload" >&2
  exit 1
fi

ERRORS=0

# --- header validation ---
HEADER=$(printf '%s' "$MSG" | head -n1)

# Rule 1: format
if ! printf '%s' "$HEADER" | grep -qE \
  '^(feat|fix|docs|style|refactor|perf|test|chore|build|ci|revert)(\([a-z0-9_-]+\))?!?: .+'; then
  echo "git-agent: header must match: <type>[(<scope>)][!]: <description>" >&2
  echo "git-agent: valid types: feat, fix, docs, style, refactor, perf, test, chore, build, ci, revert" >&2
  ERRORS=$((ERRORS + 1))
fi

# Rule 3: title <=50 chars
HEADER_LEN=$(printf '%s' "$HEADER" | awk '{print length}')
if [ "$HEADER_LEN" -gt 50 ]; then
  printf 'git-agent: title must be 50 characters or less (got %d)\n' "$HEADER_LEN" >&2
  ERRORS=$((ERRORS + 1))
fi

# Rule 4: title must not end with '.'
case "$HEADER" in
  *.)
    echo "git-agent: title must not end with a period" >&2
    ERRORS=$((ERRORS + 1))
    ;;
esac

# Rule 2: description must be all lowercase
DESC="${HEADER#*: }"
DESC_LOWER=$(printf '%s' "$DESC" | tr '[:upper:]' '[:lower:]')
if [ "$DESC" != "$DESC_LOWER" ]; then
  echo "git-agent: description must be all lowercase" >&2
  ERRORS=$((ERRORS + 1))
fi

# Warning W1: past-tense verb in description
FIRST_WORD=$(printf '%s' "$DESC" | awk '{print tolower($1)}')
case "$FIRST_WORD" in
  added|removed|updated|changed|fixed|created|deleted|modified|implemented|\
refactored|renamed|moved|replaced|improved|enhanced|upgraded|downgraded|\
reverted|resolved)
    printf 'git-agent: warning: description starts with past-tense verb "%s" — prefer imperative mood\n' "$FIRST_WORD" >&2
    ;;
esac

# --- body validation ---
LINE_COUNT=$(printf '%s' "$MSG" | wc -l | tr -d ' ')
if [ "$LINE_COUNT" -lt 1 ]; then
  echo "git-agent: body is required: add bullet points followed by an explanation paragraph" >&2
  ERRORS=$((ERRORS + 1))
  [ "$ERRORS" -gt 0 ] && exit 1
  exit 0
fi

# blank line after header
SECOND_LINE=$(printf '%s' "$MSG" | sed -n '2p')
if [ -n "$SECOND_LINE" ]; then
  echo "git-agent: blank line required between header and body" >&2
  ERRORS=$((ERRORS + 1))
fi

# Rule 6: body must contain bullet points
BULLET_COUNT=$(printf '%s' "$MSG" | awk 'NR>=3 && /^- /{count++} END{print count+0}')
if [ "$BULLET_COUNT" -eq 0 ]; then
  echo "git-agent: body must contain at least one bullet point starting with '- '" >&2
  ERRORS=$((ERRORS + 1))
fi

# Rule 7: body lines <=72 chars (excluding footers)
LONG_LINES=$(printf '%s' "$MSG" | awk '
  NR>=3 && !/^[A-Za-z][A-Za-z0-9 -]*: / && length($0)>72 {
    count++
  }
  END { print count+0 }
')
if [ "$LONG_LINES" -gt 0 ]; then
  echo "git-agent: body contains line(s) exceeding 72 characters" >&2
  ERRORS=$((ERRORS + 1))
fi

# Rule 8: explanation paragraph required after last bullet
if [ "$BULLET_COUNT" -gt 0 ]; then
  HAS_EXPLANATION=$(printf '%s' "$MSG" | awk '
    NR>=3 { lines[NR]=$0 }
    NR>=3 && /^- / { last_bullet=NR }
    END {
      if (last_bullet==0) { print 0; exit }
      for (i=last_bullet+1; i<=NR; i++) {
        if (lines[i]!="" && lines[i]!~/^- / &&
            lines[i]!~/^[A-Za-z][A-Za-z0-9 -]*: /) {
          print 1; exit
        }
      }
      print 0
    }
  ')
  if [ "$HAS_EXPLANATION" -eq 0 ]; then
    echo "git-agent: explanation paragraph required after bullet points" >&2
    ERRORS=$((ERRORS + 1))
  fi
fi

# Rule 9: Co-Authored-By format when present
if printf '%s' "$MSG" | grep -q '^Co-Authored-By:'; then
  if ! printf '%s' "$MSG" | grep -qE '^Co-Authored-By: .+ <[^>]+@[^>]+>$'; then
    echo "git-agent: Co-Authored-By must be: Co-Authored-By: Name <email@domain>" >&2
    ERRORS=$((ERRORS + 1))
  fi
fi

# Warning W2: past-tense verbs in bullet points
printf '%s' "$MSG" | awk '
  NR>=3 && /^- / {
    word=tolower($2)
    if (word ~ /^(added|removed|updated|changed|fixed|created|deleted|modified|implemented|refactored|renamed|moved|replaced|improved|enhanced|upgraded|downgraded|reverted|resolved)$/) {
      printf "git-agent: warning: bullet starts with past-tense verb \"%s\" \342\200\224 prefer imperative mood\n", word > "/dev/stderr"
    }
  }
' 2>&1 >&2 || true

[ "$ERRORS" -gt 0 ] && exit 1
exit 0
