package application

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/domain/graph"
	graphinfra "github.com/gitagenthq/git-agent/infrastructure/graph"
)

func setupASTRepo(t *testing.T) *graphinfra.SQLiteASTRepository {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "ast.db")
	client := graphinfra.NewSQLiteClient(dbPath)
	ctx := context.Background()
	if err := client.Open(ctx); err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := client.InitSchema(ctx); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return graphinfra.NewSQLiteASTRepository(client)
}

func TestReferenceResolver_UnambiguousResolvesToEdge(t *testing.T) {
	repo := setupASTRepo(t)
	ctx := context.Background()

	caller := graph.ASTNode{ID: "caller", Kind: graph.ASTNodeKindFunction, Name: "Caller", QualifiedName: "a.go::Caller", FilePath: "a.go", Language: "go"}
	callee := graph.ASTNode{ID: "callee", Kind: graph.ASTNodeKindFunction, Name: "Target", QualifiedName: "b.go::Target", FilePath: "b.go", Language: "go"}
	if err := repo.UpsertASTNode(ctx, caller); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertASTNode(ctx, callee); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertUnresolvedRef(ctx, graph.ASTUnresolvedRef{
		FromNodeID: "caller", ReferenceName: "Target", ReferenceKind: string(graph.ASTEdgeKindCalls), Line: 5, FilePath: "a.go", Language: "go",
	}); err != nil {
		t.Fatal(err)
	}

	resolver := NewReferenceResolver(repo, nil)
	result, err := resolver.Resolve(ctx)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if result.ResolvedCount != 1 {
		t.Errorf("expected 1 resolved, got %d", result.ResolvedCount)
	}

	callers, err := repo.GetCallers(ctx, "callee", 1)
	if err != nil {
		t.Fatalf("get callers: %v", err)
	}
	if len(callers) != 1 || callers[0].Node.ID != "caller" {
		t.Errorf("expected caller to be linked to callee, got %+v", callers)
	}
}

func TestReferenceResolver_AmbiguousWithOneExportedResolves(t *testing.T) {
	repo := setupASTRepo(t)
	ctx := context.Background()

	caller := graph.ASTNode{ID: "caller", Kind: graph.ASTNodeKindFunction, Name: "Caller", QualifiedName: "a.go::Caller", FilePath: "a.go", Language: "go"}
	priv := graph.ASTNode{ID: "priv", Kind: graph.ASTNodeKindMethod, Name: "Dup", QualifiedName: "b.go::priv.Dup", FilePath: "b.go", Language: "go", IsExported: false}
	pub := graph.ASTNode{ID: "pub", Kind: graph.ASTNodeKindFunction, Name: "Dup", QualifiedName: "c.go::Dup", FilePath: "c.go", Language: "go", IsExported: true}
	for _, n := range []graph.ASTNode{caller, priv, pub} {
		if err := repo.UpsertASTNode(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.UpsertUnresolvedRef(ctx, graph.ASTUnresolvedRef{
		FromNodeID: "caller", ReferenceName: "Dup", ReferenceKind: string(graph.ASTEdgeKindCalls), Line: 10, FilePath: "a.go", Language: "go",
	}); err != nil {
		t.Fatal(err)
	}

	resolver := NewReferenceResolver(repo, nil)
	result, err := resolver.Resolve(ctx)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if result.ResolvedCount != 1 {
		t.Errorf("expected 1 resolved (exported), got %d (amb=%d, nf=%d)", result.ResolvedCount, result.AmbiguousCount, result.NotFoundCount)
	}

	callers, err := repo.GetCallers(ctx, "pub", 1)
	if err != nil {
		t.Fatalf("get callers: %v", err)
	}
	if len(callers) != 1 {
		t.Errorf("expected exported symbol to have 1 caller, got %d", len(callers))
	}
}

func TestReferenceResolver_AmbiguousAllNonExportedLeavesUnresolved(t *testing.T) {
	repo := setupASTRepo(t)
	ctx := context.Background()

	caller := graph.ASTNode{ID: "caller", Kind: graph.ASTNodeKindFunction, Name: "Caller", QualifiedName: "a.go::Caller", FilePath: "a.go", Language: "go"}
	dup1 := graph.ASTNode{ID: "dup1", Kind: graph.ASTNodeKindMethod, Name: "Conf", QualifiedName: "b.go::dup1.Conf", FilePath: "b.go", Language: "go"}
	dup2 := graph.ASTNode{ID: "dup2", Kind: graph.ASTNodeKindMethod, Name: "Conf", QualifiedName: "c.go::dup2.Conf", FilePath: "c.go", Language: "go"}
	for _, n := range []graph.ASTNode{caller, dup1, dup2} {
		if err := repo.UpsertASTNode(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.UpsertUnresolvedRef(ctx, graph.ASTUnresolvedRef{
		FromNodeID: "caller", ReferenceName: "Conf", ReferenceKind: string(graph.ASTEdgeKindCalls), Line: 3, FilePath: "a.go", Language: "go",
	}); err != nil {
		t.Fatal(err)
	}

	resolver := NewReferenceResolver(repo, nil)
	result, err := resolver.Resolve(ctx)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if result.ResolvedCount != 0 {
		t.Errorf("expected 0 resolved, got %d", result.ResolvedCount)
	}
	if result.AmbiguousCount != 1 {
		t.Errorf("expected 1 ambiguous, got %d", result.AmbiguousCount)
	}
}

func TestReferenceResolver_NoMatchLeavesUnresolved(t *testing.T) {
	repo := setupASTRepo(t)
	ctx := context.Background()

	caller := graph.ASTNode{ID: "caller", Kind: graph.ASTNodeKindFunction, Name: "Caller", QualifiedName: "a.go::Caller", FilePath: "a.go", Language: "go"}
	if err := repo.UpsertASTNode(ctx, caller); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertUnresolvedRef(ctx, graph.ASTUnresolvedRef{
		FromNodeID: "caller", ReferenceName: "Ghost", ReferenceKind: string(graph.ASTEdgeKindCalls), Line: 1, FilePath: "a.go", Language: "go",
	}); err != nil {
		t.Fatal(err)
	}

	resolver := NewReferenceResolver(repo, nil)
	result, err := resolver.Resolve(ctx)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if result.NotFoundCount != 1 {
		t.Errorf("expected 1 not-found, got %d", result.NotFoundCount)
	}
	if result.ResolvedCount != 0 {
		t.Errorf("expected 0 resolved, got %d", result.ResolvedCount)
	}
}

// TestReferenceResolver_KnownNamesPrefilter verifies that references whose
// trailing name appears nowhere in the symbol table are counted as not-found
// without needing a per-ref DB lookup.
func TestReferenceResolver_KnownNamesPrefilter(t *testing.T) {
	repo := setupASTRepo(t)
	ctx := context.Background()

	caller := graph.ASTNode{ID: "caller", Kind: graph.ASTNodeKindFunction, Name: "Caller", QualifiedName: "a.go::Caller", FilePath: "a.go", Language: "go"}
	if err := repo.UpsertASTNode(ctx, caller); err != nil {
		t.Fatal(err)
	}
	// stdlib-style qualified calls — their trailing names don't exist locally.
	for _, ref := range []string{"fmt.Println", "os.Exit", "strings.ToUpper"} {
		if err := repo.UpsertUnresolvedRef(ctx, graph.ASTUnresolvedRef{
			FromNodeID: "caller", ReferenceName: ref, ReferenceKind: string(graph.ASTEdgeKindCalls), Line: 1, FilePath: "a.go", Language: "go",
		}); err != nil {
			t.Fatal(err)
		}
	}

	resolver := NewReferenceResolver(repo, nil)
	result, err := resolver.Resolve(ctx)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if result.NotFoundCount != 3 {
		t.Errorf("expected 3 not-found (all external), got %d", result.NotFoundCount)
	}
	if result.ResolvedCount != 0 {
		t.Errorf("expected 0 resolved, got %d", result.ResolvedCount)
	}
}

func TestReferenceResolver_CrossFileCallAfterIndex(t *testing.T) {
	repo := setupASTRepo(t)
	ctx := context.Background()

	fileA := graph.ASTNode{ID: "func:runIt", Kind: graph.ASTNodeKindFunction, Name: "runIt", QualifiedName: "a.go::runIt", FilePath: "a.go", Language: "go"}
	fileB := graph.ASTNode{ID: "func:helper", Kind: graph.ASTNodeKindFunction, Name: "helper", QualifiedName: "b.go::helper", FilePath: "b.go", Language: "go", IsExported: true}
	for _, n := range []graph.ASTNode{fileA, fileB} {
		if err := repo.UpsertASTNode(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.UpsertUnresolvedRef(ctx, graph.ASTUnresolvedRef{
		FromNodeID: "func:runIt", ReferenceName: "helper", ReferenceKind: string(graph.ASTEdgeKindCalls), Line: 7, FilePath: "a.go", Language: "go",
	}); err != nil {
		t.Fatal(err)
	}

	resolver := NewReferenceResolver(repo, nil)
	if _, err := resolver.Resolve(ctx); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	callers, err := repo.GetCallers(ctx, "func:helper", 1)
	if err != nil {
		t.Fatalf("get callers: %v", err)
	}
	if len(callers) != 1 {
		t.Fatalf("expected 1 cross-file caller, got %d", len(callers))
	}
	if callers[0].Node.FilePath != "a.go" {
		t.Errorf("expected caller from a.go, got %s", callers[0].Node.FilePath)
	}
}

func TestPickCandidate(t *testing.T) {
	if got := pickCandidate(nil, ""); got != nil {
		t.Error("nil candidates should return nil")
	}
	single := []graph.ASTNode{{ID: "s"}}
	if got := pickCandidate(single, ""); got == nil || got.ID != "s" {
		t.Error("single candidate should resolve")
	}
	multi := []graph.ASTNode{{ID: "a"}, {ID: "b", IsExported: true}}
	if got := pickCandidate(multi, ""); got == nil || got.ID != "b" {
		t.Error("multiple with one exported should pick exported")
	}
	multiExp := []graph.ASTNode{{ID: "a", IsExported: true}, {ID: "b", IsExported: true}}
	if got := pickCandidate(multiExp, ""); got != nil {
		t.Error("multiple exported should be ambiguous (nil)")
	}
	multiPriv := []graph.ASTNode{{ID: "a"}, {ID: "b"}}
	if got := pickCandidate(multiPriv, ""); got != nil {
		t.Error("multiple non-exported should be ambiguous (nil)")
	}
}

func TestPickCandidate_QualifierReceiverMatch(t *testing.T) {
	// Two exported methods named "Commit" on different types. Qualifier "svc"
	// matches the receiver type "CommitService" in the qualified name.
	candidates := []graph.ASTNode{
		{ID: "a", Name: "Commit", QualifiedName: "app/commit_service.go::CommitService.Commit", IsExported: true},
		{ID: "b", Name: "Commit", QualifiedName: "git/client.go::Client.Commit", IsExported: true},
	}
	if got := pickCandidate(candidates, "CommitService"); got == nil || got.ID != "a" {
		t.Errorf("qualifier matching receiver type should resolve: got %v", got)
	}
	if got := pickCandidate(candidates, "Client"); got == nil || got.ID != "b" {
		t.Errorf("qualifier matching Client receiver should resolve: got %v", got)
	}
	if got := pickCandidate(candidates, "unknown"); got != nil {
		t.Error("non-matching qualifier with multiple exported should be ambiguous")
	}
}

func TestPickCandidate_QualifierPackageMatch(t *testing.T) {
	candidates := []graph.ASTNode{
		{ID: "a", Name: "ToUpper", QualifiedName: "strings/strings.go::ToUpper", FilePath: "strings/strings.go", IsExported: true},
		{ID: "b", Name: "ToUpper", QualifiedName: "util/case.go::ToUpper", FilePath: "util/case.go", IsExported: true},
	}
	if got := pickCandidate(candidates, "strings"); got == nil || got.ID != "a" {
		t.Errorf("qualifier matching package name should resolve: got %v", got)
	}
}

func TestReferenceResolver_QualifiedSelectorResolves(t *testing.T) {
	repo := setupASTRepo(t)
	ctx := context.Background()

	caller := graph.ASTNode{ID: "caller", Kind: graph.ASTNodeKindFunction, Name: "runCommit", QualifiedName: "cmd/commit.go::runCommit", FilePath: "cmd/commit.go", Language: "go"}
	svcCommit := graph.ASTNode{ID: "svcCommit", Kind: graph.ASTNodeKindMethod, Name: "Commit", QualifiedName: "app/commit_service.go::CommitService.Commit", FilePath: "app/commit_service.go", Language: "go", IsExported: true}
	clientCommit := graph.ASTNode{ID: "clientCommit", Kind: graph.ASTNodeKindMethod, Name: "Commit", QualifiedName: "git/client.go::Client.Commit", FilePath: "git/client.go", Language: "go", IsExported: true}
	for _, n := range []graph.ASTNode{caller, svcCommit, clientCommit} {
		if err := repo.UpsertASTNode(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	// Qualified call whose qualifier is the receiver type name. This is the
	// form that can be disambiguated without type inference (e.g. a call
	// written as `CommitService.Commit` or a package-qualified `strings.ToUpper`).
	if err := repo.UpsertUnresolvedRef(ctx, graph.ASTUnresolvedRef{
		FromNodeID: "caller", ReferenceName: "CommitService.Commit", ReferenceKind: string(graph.ASTEdgeKindCalls), Line: 5, FilePath: "cmd/commit.go", Language: "go",
	}); err != nil {
		t.Fatal(err)
	}

	resolver := NewReferenceResolver(repo, nil)
	result, err := resolver.Resolve(ctx)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if result.ResolvedCount != 1 {
		t.Fatalf("expected 1 resolved, got %d (amb=%d, nf=%d)", result.ResolvedCount, result.AmbiguousCount, result.NotFoundCount)
	}

	callers, err := repo.GetCallers(ctx, "svcCommit", 1)
	if err != nil {
		t.Fatalf("get callers: %v", err)
	}
	if len(callers) != 1 || callers[0].Node.ID != "caller" {
		t.Errorf("expected svcCommit to have caller linked, got %+v", callers)
	}
}

func TestReferenceResolver_VarCallHintUsesFactoryReturnType(t *testing.T) {
	repo := setupASTRepo(t)
	ctx := context.Background()

	nodes := []graph.ASTNode{
		{ID: "caller", Kind: graph.ASTNodeKindFunction, Name: "run", QualifiedName: "run.go::run", FilePath: "run.go", Language: "go"},
		{ID: "factory", Kind: graph.ASTNodeKindFunction, Name: "NewClient", QualifiedName: "types.go::NewClient", FilePath: "types.go", Language: "go", ReturnType: "Client", IsExported: true},
		{ID: "clientConnect", Kind: graph.ASTNodeKindMethod, Name: "Connect", QualifiedName: "types.go::Client.Connect", FilePath: "types.go", Language: "go", IsExported: true},
		{ID: "serverConnect", Kind: graph.ASTNodeKindMethod, Name: "Connect", QualifiedName: "types.go::Server.Connect", FilePath: "types.go", Language: "go", IsExported: true},
	}
	for _, n := range nodes {
		if err := repo.UpsertASTNode(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.UpsertUnresolvedRef(ctx, graph.ASTUnresolvedRef{
		FromNodeID:    "caller",
		ReferenceName: "svc.Connect",
		ReferenceKind: string(graph.ASTEdgeKindCalls),
		Line:          7,
		FilePath:      "run.go",
		Language:      "go",
		VarCallHint:   "NewClient",
	}); err != nil {
		t.Fatal(err)
	}

	resolver := NewReferenceResolver(repo, nil)
	result, err := resolver.Resolve(ctx)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if result.ResolvedCount != 1 {
		t.Fatalf("expected exactly 1 resolved, got %d (amb=%d, nf=%d)", result.ResolvedCount, result.AmbiguousCount, result.NotFoundCount)
	}

	clientCallers, err := repo.GetCallers(ctx, "clientConnect", 1)
	if err != nil {
		t.Fatalf("get client callers: %v", err)
	}
	if len(clientCallers) != 1 || clientCallers[0].Node.ID != "caller" {
		t.Fatalf("expected caller linked to Client.Connect, got %+v", clientCallers)
	}
	serverCallers, err := repo.GetCallers(ctx, "serverConnect", 1)
	if err != nil {
		t.Fatalf("get server callers: %v", err)
	}
	if len(serverCallers) != 0 {
		t.Fatalf("should not link caller to Server.Connect, got %+v", serverCallers)
	}
}

func TestASTImpactService_DeterministicSeedForDuplicateNames(t *testing.T) {
	repo := setupASTRepo(t)
	ctx := context.Background()

	// Insert in reverse lexical order to ensure ImpactBySymbol does not depend
	// on SQLite's row order for same-named symbols.
	server := graph.ASTNode{ID: "serverConnect", Kind: graph.ASTNodeKindMethod, Name: "Connect", QualifiedName: "z.go::Server.Connect", FilePath: "z.go", Language: "go", IsExported: true}
	client := graph.ASTNode{ID: "clientConnect", Kind: graph.ASTNodeKindMethod, Name: "Connect", QualifiedName: "a.go::Client.Connect", FilePath: "a.go", Language: "go", IsExported: true}
	for _, n := range []graph.ASTNode{server, client} {
		if err := repo.UpsertASTNode(ctx, n); err != nil {
			t.Fatal(err)
		}
	}

	result, err := NewASTImpactService(repo).ImpactBySymbol(ctx, "Connect", 1)
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	if result.SeedNode.ID != "clientConnect" {
		t.Fatalf("expected deterministic seed clientConnect, got %+v", result.SeedNode)
	}
}

func TestASTImpactService_FindsCallers(t *testing.T) {
	repo := setupASTRepo(t)
	ctx := context.Background()

	seed := graph.ASTNode{ID: "target", Name: "runHandler", QualifiedName: "pkg.runHandler", Kind: graph.ASTNodeKindFunction, FilePath: "handler.go", Language: "go"}
	caller := graph.ASTNode{ID: "caller", Name: "main", QualifiedName: "pkg.main", Kind: graph.ASTNodeKindFunction, FilePath: "main.go", Language: "go"}
	for _, n := range []graph.ASTNode{seed, caller} {
		if err := repo.UpsertASTNode(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.UpsertASTEdge(ctx, graph.ASTEdge{Source: caller.ID, Target: seed.ID, Kind: graph.ASTEdgeKindCalls}); err != nil {
		t.Fatal(err)
	}

	result, err := NewASTImpactService(repo).ImpactBySymbol(ctx, "runHandler", 1)
	if err != nil {
		t.Fatalf("ImpactBySymbol: %v", err)
	}
	if result.TotalFound != 1 || len(result.Impacted) != 1 {
		t.Fatalf("expected 1 impacted entry, got %+v", result)
	}
	if result.Impacted[0].Node.Name != "main" {
		t.Fatalf("expected impacted main, got %+v", result.Impacted[0].Node)
	}
}

func TestASTImpactService_SymbolNotFound(t *testing.T) {
	repo := setupASTRepo(t)
	ctx := context.Background()
	_, err := NewASTImpactService(repo).ImpactBySymbol(ctx, "nonexistent", 1)
	if err == nil {
		t.Fatal("expected error for nonexistent symbol")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error should mention not found: %v", err)
	}
}
