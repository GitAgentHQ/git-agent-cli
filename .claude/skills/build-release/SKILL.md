---
name: build-release
description: Build git-agent with embedded credentials (CLIENT_TOKEN, WORKER_URL, MODEL) via scripts/build.sh. Use when you need a fully functional binary for manual testing or pre-release verification.
disable-model-invocation: true
---

Build the binary with embedded credentials from `.env`:

```bash
bash scripts/build.sh
```

This script:
1. Loads `.env` (or uses CI env vars if already set)
2. Validates that `CLIENT_TOKEN`, `WORKER_URL`, and `MODEL` are present
3. Compiles with `-ldflags` to embed credentials and strip debug symbols
4. Outputs the binary to the name specified by `OUTPUT` (defaults to `git-agent`)

If the build fails due to missing env vars, check that `.env` exists and contains the required keys. See `.env.example` for the expected format.
