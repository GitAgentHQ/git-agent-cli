#!/bin/sh
# conventional commit hook - validates commit message format
# $1 = commit message
MSG="$1"
if echo "$MSG" | grep -qE '^(feat|fix|docs|style|refactor|perf|test|chore|build|ci|revert)(\(.+\))?: .+'; then
    exit 0
fi
echo "ga: commit message does not follow Conventional Commits format" >&2
echo "ga: expected: <type>(<scope>): <description>" >&2
exit 1
