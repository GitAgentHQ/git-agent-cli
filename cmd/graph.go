package cmd

import (
	"github.com/spf13/cobra"
)

// graphCmd is the parent for every command that reads or audits the agent
// Event Log and its derived indexes (co-change, AST). No graph read/audit
// command lives at the top level — see CLAUDE.md "Command Surface Conventions".
var graphCmd = &cobra.Command{
	Use:   "graph",
	Short: "Query and audit the agent Event Log",
	Long: `Query and audit the agent Event Log and its derived indexes (co-change,
AST). The Event Log is the append-only, hash-chained record of every captured
agent and human action; the graph indexes are its derived projections.

Start from timeline for a broad view of recent action history, then drill in:
  status, verify, rebuild, sync — index health, chain integrity audit, repair, and sync
  timeline, impact         — action history and co-change / structural impact
  diagnose, provenance     — regression tracing and file provenance
  callers, callees, node   — AST call-graph traversal and symbol lookup
  query, affected          — symbol search and test-files-affected tracing

Do not re-derive what the graph already holds: do not hand-walk git log to
reconstruct history (timeline/provenance already did it), do not re-verify the
chain after verify reports ok, and do not run rebuild to check freshness
(status reports the last indexed commit; sync brings projections up to date
without a full replay when already current).
`,
}

func init() {
	rootCmd.AddCommand(graphCmd)
}
