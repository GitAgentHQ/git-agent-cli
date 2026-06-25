package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/graph"
	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
	infraGraph "github.com/gitagenthq/git-agent/infrastructure/graph"
	"github.com/gitagenthq/git-agent/infrastructure/redact"
	"github.com/gitagenthq/git-agent/pkg/output"
)

var captureCmd = &cobra.Command{
	Use:    "capture",
	Short:  "Record an agent action into the graph",
	Hidden: true,
	RunE:   runCapture,
}

func runCapture(cmd *cobra.Command, args []string) error {
	source, _ := cmd.Flags().GetString("source")
	tool, _ := cmd.Flags().GetString("tool")
	instanceID, _ := cmd.Flags().GetString("instance-id")
	message, _ := cmd.Flags().GetString("message")
	endSession, _ := cmd.Flags().GetBool("end-session")

	// When invoked as a Claude Code PostToolUse hook, the full payload arrives as
	// JSON on stdin. Build the redacted EventRecord to append; fold the tool name
	// and session id into the flags too, for callers that only want those.
	stdin := readPipedStdin()
	tool, instanceID = mergeHookPayload(tool, instanceID, stdin)

	if source == "" {
		return fmt.Errorf("capture: --source is required")
	}

	req := graph.CaptureRequest{
		Source:     source,
		Tool:       tool,
		InstanceID: instanceID,
		Message:    message,
		EndSession: endSession,
	}
	// On interactive/malformed stdin buildEventRecord returns ok=false; capture
	// then has no payload to append and degrades to a no-op (never errors).
	if event, toolResponse, ok := buildEventRecord(source, tool, instanceID, stdin, redact.NewRedactor()); ok {
		req.Event = &event
		req.ToolResponse = toolResponse
	}

	result, err := captureOnce(cmd, req)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "capture: warning: %v\n", err)
		return nil
	}
	if result != nil {
		_ = output.EncodeJSON(os.Stdout, result)
	}
	return nil
}

func captureOnce(cmd *cobra.Command, req graph.CaptureRequest) (*graph.CaptureResult, error) {
	ctx := cmd.Context()

	gitClient := infraGit.NewClient()
	root, err := gitClient.RepoRoot(ctx)
	if err != nil {
		return nil, fmt.Errorf("repo root: %w", err)
	}

	dbPath := filepath.Join(root, ".git-agent", "graph.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create .git-agent dir: %w", err)
	}

	client := infraGraph.NewSQLiteClient(dbPath)
	repo := infraGraph.NewSQLiteRepository(client)
	if err := repo.Open(ctx); err != nil {
		return nil, fmt.Errorf("open graph db: %w", err)
	}
	defer repo.Close()
	if err := client.ValidateSchemaVersion(ctx); err != nil {
		return nil, err
	}
	if err := repo.InitSchema(ctx); err != nil {
		return nil, fmt.Errorf("init schema: %w", err)
	}

	graphGit := infraGit.NewGraphClient(root)
	captureSvc := application.NewCaptureService(
		repo, graphGit,
		infraGraph.NewUUIDSessionIDGenerator(),
	)

	return captureSvc.Capture(ctx, req)
}

func init() {
	captureCmd.Flags().String("source", "", "action source (claude-code, cursor, human, etc.)")
	captureCmd.Flags().String("tool", "", "tool name (Edit, Write, Bash, etc.)")
	captureCmd.Flags().String("instance-id", "", "instance identifier for concurrent sessions")
	captureCmd.Flags().String("message", "", "optional human message")
	captureCmd.Flags().Bool("end-session", false, "end the active session without creating a new action")

	rootCmd.AddCommand(captureCmd)
}
