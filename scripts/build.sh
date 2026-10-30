#!/usr/bin/env bash
set -euo pipefail

: "${OUTPUT:=git-agent}"

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"

# No endpoint is embedded: the binary carries the version and nothing else, so
# every install configures its own base_url and model.
LDFLAGS="-s -w -X github.com/gitagenthq/git-agent/cmd.buildVersion=${VERSION}"

go build -ldflags "${LDFLAGS}" -o "${OUTPUT}" .
