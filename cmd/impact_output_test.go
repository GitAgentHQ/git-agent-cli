package cmd

import (
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/domain/graph"
)

func TestFormatImpactLine_DirectHasNoMarker(t *testing.T) {
	line := formatImpactLine(graph.ImpactEntry{Path: "a.go", CouplingStrength: 0.5, CouplingCount: 4, Depth: 1}, 5)
	if strings.Contains(line, "indirect") || strings.Contains(line, "depth") {
		t.Errorf("direct coupling should carry no depth marker: %q", line)
	}
	if !strings.Contains(line, "50%") || !strings.Contains(line, "(4 co-changes)") {
		t.Errorf("line missing strength/count: %q", line)
	}
}

func TestFormatImpactLine_IndirectIsMarked(t *testing.T) {
	line := formatImpactLine(graph.ImpactEntry{Path: "c.go", CouplingStrength: 0.5, CouplingCount: 6, Depth: 2}, 5)
	if !strings.Contains(line, "[indirect, depth 2]") {
		t.Errorf("transitive coupling must be marked with its depth: %q", line)
	}
}
