package cmd

import (
	"github.com/spf13/cobra"
)

// graphCmd is the parent for read-only queries over the deterministic code
// graph: the AST (structure) and the commit-history co-change index. Forensic
// queries over the append-only agent Event Log live under `audit`, not here.
// No graph read command lives at the top level — see CLAUDE.md "Command Surface
// Conventions".
var graphCmd = &cobra.Command{
	Use:   "graph",
	Short: "Query code structure and change-coupling (read-only, offline)",
	Long: `Query the deterministic code graph: the AST (symbols and their call
structure) and the commit-history co-change index (which files change together).
Both are re-derivable from the current repository state; all queries are
read-only, offline, and need no API key.

  search, symbol           — full-text symbol search and symbol lookup
  callers, callees         — AST call-graph traversal (incoming / outgoing)
  external-refs, affected  — external-package call sites and tests-affected
  impact                   — co-change coupling for files
  status                   — index health and row counts

For forensic queries over the agent Event Log (action history, regression
tracing, file provenance, chain-integrity audit) use ` + "`git-agent audit`" + `.

Do not re-derive what the graph already holds: do not hand-walk git log to
reconstruct co-change, and do not run ` + "`graph index`" + ` to check freshness
(status reports the last indexed commit; reads sync themselves).
`,
}

func init() {
	addOutputFlag(graphCmd, true)
	rootCmd.AddCommand(graphCmd)
}
