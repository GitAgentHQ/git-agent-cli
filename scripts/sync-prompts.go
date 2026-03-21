//go:build ignore

// sync-prompts.go prints all static system prompts used by the CLI,
// delimited by "\n---\n", for syncing to the proxy's ALLOWED_SYSTEM_PROMPTS secret.
//
// Usage:
//
//	go run scripts/sync-prompts.go | wrangler secret put ALLOWED_SYSTEM_PROMPTS --name git-agent-proxy
package main

import (
	"fmt"
	"strings"

	"github.com/gitagenthq/git-agent/infrastructure/openai"
)

func main() {
	fmt.Print(strings.Join(openai.AllSystemPrompts(), "\n---\n"))
}
