package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/graph"
	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
	infraGraph "github.com/gitagenthq/git-agent/infrastructure/graph"
	"github.com/gitagenthq/git-agent/pkg/output"
)

var timelineCmd = &cobra.Command{
	Use:   "timeline",
	Short: "Show agent and human action history",
	Long: `Show recent agent and human action history as sessions of actions, each
with the files it touched. The broad entry point over the Event Log: start here
to see what happened, then drill into ` + "`provenance`" + ` for one file's full
history or ` + "`diagnose`" + ` to trace a regression. Filter with --file, --source,
or --since. Read-only.`,
	RunE: runTimeline,
}

func runTimeline(cmd *cobra.Command, args []string) error {
	sinceStr, _ := cmd.Flags().GetString("since")
	source, _ := cmd.Flags().GetString("source")
	file, _ := cmd.Flags().GetString("file")
	top, _ := cmd.Flags().GetInt("top")
	jsonFlag, _ := cmd.Flags().GetBool("json")
	textFlag, _ := cmd.Flags().GetBool("text")

	ctx := cmd.Context()

	gitClient := infraGit.NewClient()
	root, err := gitClient.RepoRoot(ctx)
	if err != nil {
		return fmt.Errorf("repo root: %w", err)
	}

	dbPath := infraGraph.DBPath(root)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("create .git-agent dir: %w", err)
	}
	// Ensure .gitignore ignores graph.db before the timeline read creates it,
	// defending repos where init has not run yet.
	if err := application.EnsureGitAgentIgnoredAt(root); err != nil {
		return fmt.Errorf("ensure gitignore: %w", err)
	}
	// Untrack graph.db if a prior commit tracked it, so the loop breaks even
	// without init. Idempotent no-op when already untracked.
	if _, err := ensureGraphDBUntracked(ctx, gitClient, root); err != nil {
		return err
	}

	client := infraGraph.NewSQLiteClient(dbPath)
	repo := infraGraph.NewSQLiteRepository(client)
	if err := repo.Open(ctx); err != nil {
		return fmt.Errorf("open graph db: %w", err)
	}
	defer repo.Close()
	if err := client.ValidateSchemaVersion(ctx); err != nil {
		return err
	}
	if err := repo.InitSchema(ctx); err != nil {
		return fmt.Errorf("init schema: %w", err)
	}

	var sinceUnix int64
	if sinceStr != "" {
		sinceUnix, err = parseSince(sinceStr)
		if err != nil {
			return fmt.Errorf("parse --since: %w", err)
		}
	}

	result, err := repo.Timeline(ctx, graph.TimelineRequest{
		Since:  sinceUnix,
		Source: source,
		File:   file,
		Top:    top,
	})
	if err != nil {
		return outputError(jsonFlag, textFlag, err)
	}

	return outputTimeline(cmd, result, jsonFlag, textFlag)
}

func parseSince(s string) (int64, error) {
	// Try Go duration first (e.g., "2h", "30m").
	d, err := time.ParseDuration(s)
	if err == nil {
		return time.Now().Add(-d).Unix(), nil
	}

	// Try "Nd" format (e.g., "7d", "30d").
	if strings.HasSuffix(s, "d") {
		numStr := strings.TrimSuffix(s, "d")
		days, parseErr := strconv.Atoi(numStr)
		if parseErr == nil && days > 0 {
			return time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix(), nil
		}
	}

	// Try RFC3339.
	t, err := time.Parse(time.RFC3339, s)
	if err == nil {
		return t.Unix(), nil
	}

	return 0, fmt.Errorf("unsupported format %q: use a duration (2h, 7d) or RFC3339 date", s)
}

func outputTimeline(cmd *cobra.Command, result *graph.TimelineResult, jsonFlag, textFlag bool) error {
	if output.Decide(jsonFlag, textFlag) == output.FormatJSON {
		return output.EncodeJSON(cmd.OutOrStdout(), result)
	}
	return outputTimelineText(cmd, result)
}

func outputTimelineText(cmd *cobra.Command, result *graph.TimelineResult) error {
	out := cmd.OutOrStdout()

	if len(result.Sessions) == 0 {
		fmt.Fprintln(out, "(no sessions found)")
		fmt.Fprintf(out, "\n0 sessions, 0 actions | query: %dms\n", result.QueryMs)
		return nil
	}

	for i, sess := range result.Sessions {
		if i > 0 {
			fmt.Fprintln(out)
		}

		startTime, _ := time.Parse(time.RFC3339, sess.StartedAt)
		timeRange := startTime.Format("2006-01-02 15:04")
		if sess.EndedAt != "" {
			endTime, _ := time.Parse(time.RFC3339, sess.EndedAt)
			timeRange += "-" + endTime.Format("15:04")
		}

		fmt.Fprintf(out, "Session %s (%s, %s, %d actions)\n",
			truncateID(sess.ID), sess.Source, timeRange, sess.ActionCount)

		// Find longest tool name for alignment.
		maxToolLen := 0
		for _, a := range sess.Actions {
			if len(a.Tool) > maxToolLen {
				maxToolLen = len(a.Tool)
			}
		}
		if maxToolLen < 4 {
			maxToolLen = 4
		}

		for _, a := range sess.Actions {
			ts, _ := time.Parse(time.RFC3339, a.Timestamp)
			toolPad := strings.Repeat(" ", maxToolLen-len(a.Tool)+1)

			filesDesc := ""
			if len(a.Files) > 0 {
				filesDesc = a.Files[0]
				if len(a.Files) > 1 {
					filesDesc += fmt.Sprintf(" (+%d)", len(a.Files)-1)
				}
			}

			fmt.Fprintf(out, "  %s%s%-40s %s\n",
				a.Tool, toolPad, filesDesc, ts.Format("15:04"))
		}
	}

	fmt.Fprintf(out, "\n%d sessions, %d actions | query: %dms\n",
		result.TotalSessions, result.TotalActions, result.QueryMs)
	return nil
}

func truncateID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func init() {
	timelineCmd.Flags().String("since", "", "show actions since duration (2h, 7d) or RFC3339 date")
	timelineCmd.Flags().String("source", "", "filter by source")
	timelineCmd.Flags().String("file", "", "filter by file path")
	timelineCmd.Flags().Int("top", 50, "max sessions to show")
	timelineCmd.Flags().Bool("json", false, "force JSON output")
	timelineCmd.Flags().Bool("text", false, "force text output")
	timelineCmd.MarkFlagsMutuallyExclusive("json", "text")

	graphCmd.AddCommand(timelineCmd)
}
