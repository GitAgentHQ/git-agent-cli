#!/usr/bin/env bash
set -euo pipefail

: "${OUTPUT:=git-agent}"

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"

# GATEWAY_URL is the free shared-gateway base URL, embedded as the default so
# a zero-config binary works out of the box. It is a URL only — never a
# credential/token. Official release builds set it via CI secrets.GATEWAY_URL;
# dev builds leave it empty (users must configure a provider).
LDFLAGS="-s -w -X github.com/gitagenthq/git-agent/cmd.buildVersion=${VERSION}"
if [[ -n "${GATEWAY_URL:-}" ]]; then
  LDFLAGS+=" -X github.com/gitagenthq/git-agent/infrastructure/config.BuildBaseURL=${GATEWAY_URL}"
fi

go build -ldflags "${LDFLAGS}" -o "${OUTPUT}" .
