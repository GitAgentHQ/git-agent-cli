package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/domain/graph"
	infraGraph "github.com/gitagenthq/git-agent/infrastructure/graph"
)

// TestStatusCmd_PartialIndexPrintsPlaceholder simulates an interrupted index
// run — commit rows committed but the last-indexed marker never written
// (RecomputeCoChanged can error before SetLastIndexedCommit). `status` must
// print the "(none)" placeholder instead of a blank hash.
func TestStatusCmd_PartialIndexPrintsPlaceholder(t *testing.T) {
	root := seedTestRepo(t)
	t.Chdir(root)

	_, client, err := openGraphDB(context.Background(), root)
	if err != nil {
		t.Fatalf("openGraphDB: %v", err)
	}
	defer client.Close()
	repo := infraGraph.NewSQLiteRepository(client)
	if err := repo.UpsertCommit(context.Background(), graph.CommitNode{
		Hash: "abc123", Message: "feat: x", AuthorName: "T", AuthorEmail: "t@t.com", Timestamp: 1,
	}); err != nil {
		t.Fatalf("UpsertCommit: %v", err)
	}

	var buf bytes.Buffer
	cmd := &cobra.Command{Use: "status"}
	cmd.SetContext(context.Background())
	addOutputFlag(cmd)
	cmd.SetOut(&buf)
	if err := cmd.Flags().Set("output", "text"); err != nil {
		t.Fatalf("set output flag: %v", err)
	}
	if err := runStatus(cmd, nil); err != nil {
		t.Fatalf("status: %v", err)
	}
	if got := buf.String(); !strings.Contains(got, "Graph: indexed (last commit (none))") {
		t.Errorf("expected placeholder for partial index, got: %s", got)
	}
}

func TestGraphNeedsIndex(t *testing.T) {
	tests := []struct {
		name           string
		lastCommit     string
		commitCount    int
		wantNeedsIndex bool
	}{
		{name: "fresh graph", wantNeedsIndex: true},
		{name: "indexed graph", lastCommit: "abc123", commitCount: 1},
		{name: "partial index", commitCount: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := graphNeedsIndex(tt.lastCommit, tt.commitCount); got != tt.wantNeedsIndex {
				t.Fatalf("graphNeedsIndex(%q, %d) = %t, want %t", tt.lastCommit, tt.commitCount, got, tt.wantNeedsIndex)
			}
		})
	}
}
