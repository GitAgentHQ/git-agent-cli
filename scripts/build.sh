#!/usr/bin/env bash
set -euo pipefail

# Load .env for local builds; CI injects these as environment variables.
if [[ -f .env ]]; then
  # shellcheck source=/dev/null
  source .env
fi

: "${CLIENT_TOKEN:?CLIENT_TOKEN is required}"
: "${WORKER_URL:?WORKER_URL is required}"
: "${MODEL:?MODEL is required}"
: "${OUTPUT:=git-agent}"

go build \
  -ldflags "-s -w \
    -X github.com/gitagenthq/git-agent/infrastructure/config.BuildAPIKey=${CLIENT_TOKEN} \
    -X github.com/gitagenthq/git-agent/infrastructure/config.BuildBaseURL=${WORKER_URL} \
    -X github.com/gitagenthq/git-agent/infrastructure/config.BuildModel=${MODEL}" \
  -o "${OUTPUT}" .
