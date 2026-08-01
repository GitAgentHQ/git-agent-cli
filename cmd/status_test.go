package cmd

import "testing"

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
