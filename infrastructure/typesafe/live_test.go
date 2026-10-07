//go:build jevlive

// Live smoke check for the System One layer. It is excluded from `go test ./...`
// by the jevlive build tag so the suite never calls a model. Run it explicitly:
//
//	TYPESAFE_API_KEY=... go test -tags jevlive ./infrastructure/typesafe/...
package typesafe_test

import (
	"context"
	"os"
	"testing"

	"github.com/gitagenthq/git-agent/domain/project"
	"github.com/gitagenthq/git-agent/infrastructure/typesafe"
)

// TestLiveScopeDecider checks the whole path against the real service: key
// authentication, request shape, and answer decoding.
func TestLiveScopeDecider(t *testing.T) {
	key := os.Getenv("TYPESAFE_API_KEY")
	if key == "" {
		t.Skip("TYPESAFE_API_KEY is not set")
	}

	decider := typesafe.NewScopeDecider(typesafe.NewClient(key, "", "", 0))
	decision, err := decider.DecideScopes(context.Background(), project.ScopeDecisionRequest{
		Dirs: []string{"cmd", "internal", "web", "node_modules", "dist"},
		Files: []string{
			"main.go", "go.mod", "internal/service.go", "internal/service_test.go",
			"web/index.html", "web/app.js", "README.md",
		},
		ExistingScopes: []project.Scope{
			{Name: "cli", Description: "Command-line entry points and flag parsing in cmd/."},
			{Name: "internal", Description: "Service and domain logic in internal/; excludes the command surface."},
		},
	})
	if err != nil {
		t.Fatalf("live scope decision failed: %v", err)
	}

	t.Logf("decisions: %+v", decision.Assignments)
	if len(decision.Assignments) == 0 {
		t.Fatal("expected at least one assignment")
	}
	known := map[string]bool{"cli": true, "internal": true, "web": true, "cmd": true}
	for _, a := range decision.Assignments {
		if a.Action == project.ScopeReuse && !known[a.Scope] {
			t.Errorf("reuse of an unoffered scope %q", a.Scope)
		}
		if a.Action == project.ScopeCreate && a.Scope == "" {
			t.Errorf("create without a derived name: %+v", a)
		}
	}
}
