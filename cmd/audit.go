package cmd

import (
	"github.com/spf13/cobra"
)

// auditCmd is the parent for read-only forensic queries over the agent Event
// Log: the append-only, hash-chained record of every captured agent and human
// action. This is a distinct data source and trust model from the structural
// code graph under `graph` — Event-Log queries answer "what happened", graph
// queries answer "what the code is now".
var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Query and audit the agent event log (forensic, integrity-checked)",
	Long: `Query and audit the agent Event Log: the append-only, hash-chained record
of every captured agent and human action. All queries are read-only, offline,
and need no API key.

  timeline     — agent and human action history (start here)
  diagnose     — trace a regression to the action that introduced it
  provenance   — rename-aware change history for one file
  verify       — check the Event Log hash chain for tampering (exits 4 on break)

For structural queries over the current code (callers, symbol lookup, co-change)
use ` + "`git-agent graph`" + `.
`,
}

func init() {
	addOutputFlag(auditCmd, true)
	rootCmd.AddCommand(auditCmd)
}
