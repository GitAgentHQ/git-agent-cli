---
name: build-release
description: Build a stripped git-agent binary via scripts/build.sh, optionally embedding the free shared-gateway URL. Use for manual testing or pre-release verification.
disable-model-invocation: true
---

Build the release-style binary:

```bash
bash scripts/build.sh
```

This script:
1. Derives the version from git unless `VERSION` is provided
2. Compiles with `-ldflags` to embed the version and strip debug symbols
3. If `GATEWAY_URL` is set, embeds it as `infrastructure/config.BuildBaseURL` — the free shared-gateway default so a zero-config binary works out of the box
4. Outputs the binary to the name specified by `OUTPUT` (defaults to `git-agent`)

Provider credentials are never embedded in release artifacts — only a gateway
URL may be embedded (and even that is optional; users can always override it
via config). Never pass a token/API key to this script.
