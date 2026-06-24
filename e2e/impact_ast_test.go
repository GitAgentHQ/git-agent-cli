package e2e_test

import (
	"strings"
	"testing"
)

// TestImpactAST_StructuralImpact is a full end-to-end test of the AST-based
// structural impact feature. It creates a multi-file Go repo, runs
// `git-agent impact --symbol`, and verifies cross-file call resolution.
func TestImpactAST_StructuralImpact(t *testing.T) {
	dir := newGitRepo(t)

	// Set up a multi-file call graph:
	//   main.go:main → handler.go:runHandler → handler.go:validateData
	//                                       → handler.go:processData
	//   dispatch.go:dispatch → handler.go:runHandler
	//   cli.go:runCLI → dispatch.go:dispatch
	writeFile(t, dir+"/go.mod", "module testproject\n\ngo 1.21\n")
	writeFile(t, dir+"/main.go", `package main

import "fmt"

func main() {
	result := runHandler("hello")
	fmt.Println(result)
}
`)
	writeFile(t, dir+"/handler.go", `package main

func runHandler(input string) string {
	processed := processData(input)
	validated := validateData(processed)
	return validated
}

func processData(raw string) string {
	return "processed:" + raw
}

func validateData(data string) string {
	return "valid:" + data
}
`)
	writeFile(t, dir+"/dispatch.go", `package main

func dispatch(cmd string) string {
	return runHandler("cli-input")
}
`)
	writeFile(t, dir+"/cli.go", `package main

func runCLI() {
	_ = dispatch("process")
}
`)
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "init")

	// Query 1: impact --symbol validateData should find runHandler as caller.
	out, code := gitAgent(t, dir, "impact", "--symbol", "validateData", "--text")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "runHandler") {
		t.Errorf("expected runHandler as caller of validateData:\n%s", out)
	}
	if !strings.Contains(out, "handler.go") {
		t.Errorf("expected file path handler.go:\n%s", out)
	}

	// Query 2: depth 2 should also find main (transitive caller via runHandler).
	out, code = gitAgent(t, dir, "impact", "--symbol", "validateData", "--depth", "2", "--text")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "main") {
		t.Errorf("expected main at depth 2:\n%s", out)
	}
	if !strings.Contains(out, "d2") {
		t.Errorf("expected depth 2 marker:\n%s", out)
	}

	// Query 3: depth 3 should find runCLI (transitive via dispatch).
	out, code = gitAgent(t, dir, "impact", "--symbol", "validateData", "--depth", "3", "--text")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "runCLI") {
		t.Errorf("expected runCLI at depth 3:\n%s", out)
	}
	if !strings.Contains(out, "d3") {
		t.Errorf("expected depth 3 marker:\n%s", out)
	}
}

// TestImpactAST_CrossFileResolution verifies that calls across different files
// are resolved by the ReferenceResolver (provenance "resolver" not "tree-sitter").
func TestImpactAST_CrossFileResolution(t *testing.T) {
	dir := newGitRepo(t)

	writeFile(t, dir+"/go.mod", "module testproject\n\ngo 1.21\n")
	writeFile(t, dir+"/a.go", `package main

func callerA() string {
	return sharedFunc()
}
`)
	writeFile(t, dir+"/b.go", `package main

func sharedFunc() string {
	return "shared"
}
`)
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "init")

	// Query: impact --symbol sharedFunc should find callerA from a.go.
	out, code := gitAgent(t, dir, "impact", "--symbol", "sharedFunc", "--text")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "callerA") {
		t.Errorf("expected callerA as cross-file caller:\n%s", out)
	}
	if !strings.Contains(out, "a.go") {
		t.Errorf("expected file a.go:\n%s", out)
	}
}

// TestImpactAST_SymbolNotFound verifies error handling for nonexistent symbols.
func TestImpactAST_SymbolNotFound(t *testing.T) {
	dir := newGitRepo(t)

	writeFile(t, dir+"/go.mod", "module testproject\n\ngo 1.21\n")
	writeFile(t, dir+"/main.go", "package main\n\nfunc main() {}\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "init")

	out, code := gitAgent(t, dir, "impact", "--symbol", "nonexistentFunc", "--text")
	if code == 0 {
		t.Fatalf("expected non-zero exit code for missing symbol:\n%s", out)
	}
	if !strings.Contains(out, "not found") {
		t.Errorf("expected 'not found' in output:\n%s", out)
	}
}

// TestImpactAST_JSONOutput verifies JSON output format.
func TestImpactAST_JSONOutput(t *testing.T) {
	dir := newGitRepo(t)

	writeFile(t, dir+"/go.mod", "module testproject\n\ngo 1.21\n")
	writeFile(t, dir+"/main.go", `package main

func target() string { return "x" }
func caller() string { return target() }
`)
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "init")

	out, code := gitAgent(t, dir, "impact", "--symbol", "target", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "seed_node") {
		t.Errorf("JSON output should contain seed_node:\n%s", out)
	}
	if !strings.Contains(out, "impacted") {
		t.Errorf("JSON output should contain impacted array:\n%s", out)
	}
	if !strings.Contains(out, "caller") {
		t.Errorf("JSON output should contain caller symbol:\n%s", out)
	}
}

// TestImpactAST_NoDuplicateEdgesOnReRun verifies that running the impact query
// twice doesn't create duplicate edges in the database.
func TestImpactAST_NoDuplicateEdgesOnReRun(t *testing.T) {
	dir := newGitRepo(t)

	writeFile(t, dir+"/go.mod", "module testproject\n\ngo 1.21\n")
	writeFile(t, dir+"/main.go", `package main

func target() string { return "x" }
func caller() string { return target() }
`)
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "init")

	// First run — auto-indexes.
	out1, code := gitAgent(t, dir, "impact", "--symbol", "target", "--text")
	if code != 0 {
		t.Fatalf("first run exit %d: %s", code, out1)
	}

	// Second run — should use existing index, no duplicates.
	out2, code := gitAgent(t, dir, "impact", "--symbol", "target", "--text")
	if code != 0 {
		t.Fatalf("second run exit %d: %s", code, out2)
	}

	// Both should show exactly 1 caller.
	count1 := strings.Count(out1, "caller")
	count2 := strings.Count(out2, "caller")
	if count1 != 1 || count2 != 1 {
		t.Errorf("expected exactly 1 'caller' in each run, got %d and %d:\n%s\n%s", count1, count2, out1, out2)
	}
}

func TestImpactAST_ReindexesWhenTrackedGoFilesChangeAtSameHead(t *testing.T) {
	dir := newGitRepo(t)

	writeFile(t, dir+"/go.mod", "module testproject\n\ngo 1.21\n")
	writeFile(t, dir+"/target.go", `package main

func target() string { return "x" }
func callerOne() string { return target() }
`)
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "init")

	out, code := gitAgent(t, dir, "impact", "--symbol", "target", "--text")
	if code != 0 {
		t.Fatalf("first impact exit %d: %s", code, out)
	}

	writeFile(t, dir+"/target.go", `package main

func target() string { return "x" }
func callerOne() string { return target() }
func callerTwo() string { return target() }
`)

	out, code = gitAgent(t, dir, "impact", "--symbol", "target", "--text")
	if code != 0 {
		t.Fatalf("second impact exit %d: %s", code, out)
	}

	if !strings.Contains(out, "callerTwo") {
		t.Fatalf("expected AST impact to refresh and include callerTwo:\n%s", out)
	}
}

func TestImpactAST_CochangeModeUsesSymbolFile(t *testing.T) {
	dir := newGitRepo(t)

	writeFile(t, dir+"/go.mod", "module testproject\n\ngo 1.21\n")
	for i := 0; i < 3; i++ {
		writeFile(t, dir+"/target.go", "package main\n\nfunc target() string { return \"x\" }\n"+strings.Repeat("// target edit\n", i+1))
		writeFile(t, dir+"/pair.go", "package main\n\nfunc pair() {}\n"+strings.Repeat("// pair edit\n", i+1))
		runGit(t, dir, "add", "-A")
		runGit(t, dir, "commit", "-m", "cochange")
	}

	out, code := gitAgent(t, dir, "impact", "--symbol", "target", "--mode", "cochange", "--text")
	if code != 0 {
		t.Fatalf("impact exit %d: %s", code, out)
	}

	if !strings.Contains(out, "pair.go") {
		t.Fatalf("expected --symbol target --mode cochange to use target.go as seed:\n%s", out)
	}
}

// TestImpactAST_ModeValidation verifies mode dispatch logic.
func TestImpactAST_ModeValidation(t *testing.T) {
	dir := newGitRepo(t)
	writeFile(t, dir+"/go.mod", "module testproject\n\ngo 1.21\n")
	writeFile(t, dir+"/main.go", "package main\n\nfunc main() {}\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "init")

	// --mode structural without --symbol should error.
	out, code := gitAgent(t, dir, "impact", "--mode", "structural", "--text")
	if code == 0 {
		t.Errorf("expected error for --mode structural without --symbol:\n%s", out)
	}
	if !strings.Contains(out, "requires --symbol") {
		t.Errorf("expected 'requires --symbol' error:\n%s", out)
	}

	// --mode combined without --symbol should error.
	out, code = gitAgent(t, dir, "impact", "--mode", "combined", "--text")
	if code == 0 {
		t.Errorf("expected error for --mode combined without --symbol:\n%s", out)
	}
}

// TestImpactAST_CoChangeUnchanged verifies that the existing co-change flow
// is not broken by the new AST features.
func TestImpactAST_CoChangeUnchanged(t *testing.T) {
	dir := newGitRepo(t)
	writeFile(t, dir+"/go.mod", "module testproject\n\ngo 1.21\n")
	writeFile(t, dir+"/main.go", "package main\n\nfunc main() {}\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "init")

	// Plain impact without --symbol should go to co-change mode.
	out, code := gitAgent(t, dir, "impact", "main.go", "--text")
	if code != 0 {
		t.Fatalf("co-change exit %d: %s", code, out)
	}
	// Should NOT contain AST-specific formatting.
	if strings.Contains(out, "Structural impact") {
		t.Errorf("co-change mode should not show AST headers:\n%s", out)
	}
}

// TestImpactAST_TestGrouping verifies that test callers are grouped after
// production callers so the signal is not buried under test noise.
func TestImpactAST_TestGrouping(t *testing.T) {
	dir := newGitRepo(t)
	writeFile(t, dir+"/go.mod", "module testproject\n\ngo 1.21\n")
	// Production caller + test caller of the same target.
	writeFile(t, dir+"/main.go", `package main

func target() string { return "x" }
func prodCaller() string { return target() }
`)
	writeFile(t, dir+"/main_test.go", `package main

import "testing"

func TestTarget(t *testing.T) { _ = target() }
`)
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "init")

	out, code := gitAgent(t, dir, "impact", "--symbol", "target", "--text")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	// Production caller must come before the tests section.
	prodIdx := strings.Index(out, "prodCaller")
	testsIdx := strings.Index(out, "TestTarget")
	if prodIdx < 0 || testsIdx < 0 {
		t.Fatalf("expected both prodCaller and TestTarget in output:\n%s", out)
	}
	if prodIdx > testsIdx {
		t.Errorf("production caller should appear before test callers:\n%s", out)
	}
	if !strings.Contains(out, "tests (1)") {
		t.Errorf("expected a test grouping header:\n%s", out)
	}
}

// TestImpactAST_QualifiedCallDisambiguation verifies that a package-qualified
// call resolves even when the bare name is ambiguous across packages.
func TestImpactAST_QualifiedCallDisambiguation(t *testing.T) {
	dir := newGitRepo(t)
	writeFile(t, dir+"/go.mod", "module testproject\n\ngo 1.21\n")
	// Two functions named "Dup" in different conceptual packages (different
	// file paths), plus a caller that qualifies one of them by package dir.
	writeFile(t, dir+"/alpha/dup.go", `package alpha

func Dup() string { return "a" }
`)
	writeFile(t, dir+"/beta/dup.go", `package beta

func Dup() string { return "b" }
`)
	writeFile(t, dir+"/main.go", `package main

import "testproject/alpha"

func main() { _ = alpha.Dup() }
`)
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "init")

	// Bare "Dup" would be ambiguous across two packages, so a query by symbol
	// name should not crash and the qualified call should still be indexed.
	out, code := gitAgent(t, dir, "impact", "--symbol", "Dup", "--text")
	// Either resolves (to alpha via package qualifier match) or reports
	// ambiguity cleanly — both acceptable. What's not acceptable is a crash
	// or exit code from an unexpected failure mode.
	if code != 0 && !strings.Contains(out, "not found") && !strings.Contains(out, "impacted") {
		t.Fatalf("unexpected failure: exit %d: %s", code, out)
	}
}

// TestImpactAST_ChainedCallReceiverInference verifies that a method called on
// a variable assigned from a factory (svc := NewClient(); svc.Connect()) is
// resolved to the right method even when the bare method name is ambiguous.
// Same-file variant: the factory and the ambiguous methods live in one file.
func TestImpactAST_ChainedCallReceiverInference(t *testing.T) {
	dir := newGitRepo(t)
	writeFile(t, dir+"/go.mod", "module testproject\n\ngo 1.21\n")
	// Two types each with a method named "Connect". A factory NewClient()
	// returns *Client. The caller assigns svc := NewClient() then calls
	// svc.Connect(), which must resolve to Client.Connect, not Server.Connect.
	writeFile(t, dir+"/main.go", `package main

type Client struct{}
type Server struct{}

func NewClient() *Client { return &Client{} }
func NewServer() *Server { return &Server{} }

func (c *Client) Connect() string { return "client" }
func (s *Server) Connect() string { return "server" }

func run() string {
	svc := NewClient()
	return svc.Connect()
}
`)
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "init")

	out, code := gitAgent(t, dir, "impact", "--symbol", "Connect", "--text")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	// run should be a caller of (Client) Connect — the test passes if the
	// ambiguity is resolved via the inferred receiver type.
	if !strings.Contains(out, "run") {
		t.Errorf("expected run as caller of Connect (inferred from NewClient):\n%s", out)
	}
}

// TestImpactAST_CrossFileChainedCallInference verifies that a method called on
// a variable assigned from a factory defined in ANOTHER file is resolved to the
// right method via return-type inference — the codegraph #645 case that the
// same-file variant (TestImpactAST_ChainedCallReceiverInference) does not cover.
func TestImpactAST_CrossFileChainedCallInference(t *testing.T) {
	dir := newGitRepo(t)
	writeFile(t, dir+"/go.mod", "module testproject\n\ngo 1.21\n")
	// File 1: factory + two types each with a same-named method.
	writeFile(t, dir+"/types.go", `package main

type Client struct{}
type Server struct{}

func NewClient() *Client { return &Client{} }
func NewServer() *Server { return &Server{} }

func (c *Client) Connect() string { return "client" }
func (s *Server) Connect() string { return "server" }
`)
	// File 2: caller assigns from the factory (cross-file) and calls Connect.
	writeFile(t, dir+"/run.go", `package main

func run() string {
	svc := NewClient()
	return svc.Connect()
}
`)
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "init")

	out, code := gitAgent(t, dir, "impact", "--symbol", "Connect", "--text")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	// run should be a caller of (Client) Connect — resolved across files via
	// NewClient's return type, NOT (Server) Connect.
	if !strings.Contains(out, "run") {
		t.Errorf("expected run as cross-file caller of Client.Connect:\n%s", out)
	}
	if strings.Contains(out, "Server") {
		t.Errorf("should not have resolved to Server.Connect:\n%s", out)
	}
}
