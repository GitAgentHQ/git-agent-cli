---
name: build-release
description: Build a stripped git-agent binary via scripts/build.sh. Use for manual testing or pre-release verification.
disable-model-invocation: true
---

Build the release-style binary:

```bash
bash scripts/build.sh
```

This script:
1. Derives the version from git unless `VERSION` is provided
2. Compiles with `-ldflags` to embed the version and strip debug symbols
3. Outputs the binary to the name specified by `OUTPUT` (defaults to `git-agent`)

No endpoint and no credential is embedded in a release artifact. Every install
configures its own `base_url` and `model`. Never pass a token/API key to this
script.
