package cmd

import (
	"github.com/spf13/cobra"
)

// graphCmd is the parent for Event Log audit subcommands (verify, provenance,
// rebuild). It groups the read/forensic surfaces over the hash-chained graph.db.
var graphCmd = &cobra.Command{
	Use:   "graph",
	Short: "Inspect and audit the agent Event Log",
}

func init() {
	rootCmd.AddCommand(graphCmd)
}
