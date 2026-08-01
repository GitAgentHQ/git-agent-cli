package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/pkg/output"
)

// outputValue is a flag value enforcing the {auto,json,text} enum for the shared
// -o/--output flag. It satisfies pflag.Value, so invalid values are rejected at
// parse time with a clear message rather than silently falling back to auto.
type outputValue string

func (o *outputValue) String() string { return string(*o) }

func (o *outputValue) Set(v string) error {
	switch v {
	case "auto", "json", "text":
		*o = outputValue(v)
		return nil
	default:
		return fmt.Errorf("invalid output format %q (want auto, json, or text)", v)
	}
}

func (o *outputValue) Type() string { return "format" }

// addOutputFlag registers the shared -o/--output flag defaulting to auto (JSON
// when stdout is piped, text on a TTY).
func addOutputFlag(cmd *cobra.Command) {
	addOutputFlagWithDefault(cmd, "auto")
}

// addOutputFlagWithDefault is addOutputFlag with an explicit default value.
// Action commands (commit, version) default to "text" so piping a human-facing
// command does not silently switch it to JSON; an agent opts in with -o json.
// Query commands default to "auto".
func addOutputFlagWithDefault(cmd *cobra.Command, def string) {
	v := outputValue(def)
	cmd.Flags().VarP(&v, "output", "o", "output format: auto, json, or text")
}

// outputFormat resolves the selected format from the -o/--output flag. The flag
// is registered locally on each command, so commands without it get auto.
func outputFormat(cmd *cobra.Command) output.Format {
	f := cmd.Flags().Lookup("output")
	if f == nil {
		return output.Decide("auto")
	}
	return output.Decide(f.Value.String())
}
