#!/bin/sh
# Installed by git-agent init --install-hook
# Delegates commit-msg validation to git-agent, then chains to any pre-existing hook.
if command -v git-agent >/dev/null 2>&1; then
  git-agent hook run commit-msg "$1" || exit $?
fi
# Chain to original hook if preserved.
SELF_DIR="$(dirname "$0")"
if [ -x "$SELF_DIR/commit-msg.pre-git-agent" ]; then
  exec "$SELF_DIR/commit-msg.pre-git-agent" "$@"
fi
