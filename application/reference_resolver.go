package application

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// ReferenceResolverResult holds the outcome of a resolution pass.
type ReferenceResolverResult struct {
	ResolvedCount  int
	AmbiguousCount int
	NotFoundCount  int
	DurationMs     int64
}

// ReferenceResolver matches unresolved cross-file references against the global
// symbol table (ast_nodes) and creates resolved ast_edges for matches.
type ReferenceResolver struct {
	repo graph.ASTRepository
	log  *slog.Logger
}

func NewReferenceResolver(repo graph.ASTRepository, log *slog.Logger) *ReferenceResolver {
	if log == nil {
		log = slog.Default()
	}
	return &ReferenceResolver{repo: repo, log: log}
}

// Resolve iterates all unresolved refs, looks up candidates by name, and
// creates ast_edges for unambiguous matches.
//
// Resolution strategy:
//   - 1 candidate → resolve
//   - multiple candidates, exactly 1 exported → resolve to that
//   - multiple candidates, none/multiple exported → ambiguous, skip
//   - 0 candidates → not found, skip
func (r *ReferenceResolver) Resolve(ctx context.Context) (*ReferenceResolverResult, error) {
	start := time.Now()

	refs, err := r.repo.ListUnresolvedRefs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list unresolved refs: %w", err)
	}

	result := &ReferenceResolverResult{}

	// knownNames pre-filter: load all distinct symbol names once so each ref
	// can skip the DB lookup for names that exist nowhere (e.g. stdlib calls
	// like fmt.Println, os.Exit). This is an O(1) Set check vs a per-ref
	// indexed query — a large win on big codebases with many external refs.
	knownNames, err := r.loadKnownNames(ctx)
	if err != nil {
		r.log.Debug("failed to load known names, falling back to per-ref lookup", "err", err)
		knownNames = nil
	}

	// Cache name lookups to avoid re-querying the same reference name.
	cache := make(map[string][]graph.ASTNode)
	// receiverType caches factory-name → inferred return type, so chained
	// calls sharing a factory resolve its type once.
	receiverType := make(map[string]string)

	for _, ref := range refs {
		// Qualified selector calls (e.g. "svc.Commit" or "fmt.Println") keep
		// their qualifier. Resolve on the trailing method/function part, then
		// use the qualifier to disambiguate across packages/types.
		lookupName, qualifier := splitReference(ref.ReferenceName)

		// If the qualifier is a local variable assigned from a factory call
		// (svc := NewClient()), resolve the factory's return type and use it as
		// the qualifier so `svc.Commit` disambiguates to `Client.Commit`.
		if ref.VarCallHint != "" {
			if rt, ok := receiverType[ref.VarCallHint]; ok {
				qualifier = rt
			} else {
				rt, err := r.resolveReceiverType(ctx, ref.VarCallHint, cache)
				if err == nil && rt != "" {
					receiverType[ref.VarCallHint] = rt
					qualifier = rt
				} else {
					receiverType[ref.VarCallHint] = ""
				}
			}
		}

		// O(1) pre-filter: if the name exists nowhere in the index, count it
		// as not-found without a DB round-trip.
		if knownNames != nil && !knownNames[lookupName] {
			result.NotFoundCount++
			continue
		}

		candidates, ok := cache[lookupName]
		if !ok {
			candidates, err = r.repo.GetASTNodeByName(ctx, lookupName)
			if err != nil {
				r.log.Debug("lookup failed for reference", "name", lookupName, "err", err)
				result.NotFoundCount++
				continue
			}
			cache[lookupName] = candidates
		}

		target := pickCandidate(candidates, qualifier)
		if target == nil {
			if len(candidates) == 0 {
				result.NotFoundCount++
			} else {
				result.AmbiguousCount++
			}
			continue
		}

		edge := graph.ASTEdge{
			Source:     ref.FromNodeID,
			Target:     target.ID,
			Kind:       graph.ASTEdgeKind(ref.ReferenceKind),
			Line:       ref.Line,
			Column:     ref.Column,
			Provenance: "resolver",
		}
		if edge.Kind == "" {
			edge.Kind = graph.ASTEdgeKindCalls
		}
		// Promote the edge kind based on the resolved target's kind, so the
		// graph captures constructor/implementation intent semantically rather
		// than just "calls": a call whose target is a class/struct is a
		// construction (instantiates), and an extends whose target is an
		// interface/trait is a conformance (implements).
		edge.Kind = promoteEdgeKind(edge.Kind, target.Kind)

		if err := r.repo.UpsertASTEdge(ctx, edge); err != nil {
			r.log.Debug("upsert edge failed", "source", ref.FromNodeID, "target", target.ID, "err", err)
			continue
		}
		result.ResolvedCount++
	}

	result.DurationMs = time.Since(start).Milliseconds()
	return result, nil
}

// promoteEdgeKind reclassifies a resolved edge based on the resolved target's
// kind, mirroring the extraction-time promotion for unresolved refs that are
// only resolved later. A call to a class/struct/interface is a construction
// (instantiates); an extends/implements edge whose target is an interface or
// trait is a conformance (implements).
func promoteEdgeKind(kind graph.ASTEdgeKind, targetKind graph.ASTNodeKind) graph.ASTEdgeKind {
	switch kind {
	case graph.ASTEdgeKindCalls:
		if isInstantiableTargetKind(targetKind) {
			return graph.ASTEdgeKindInstantiates
		}
	case graph.ASTEdgeKindExtends:
		if isConformanceTargetKind(targetKind) {
			return graph.ASTEdgeKindImplements
		}
	}
	return kind
}

// isInstantiableTargetKind reports whether a target kind represents a type
// that a bare call constructs (class/struct/interface).
func isInstantiableTargetKind(kind graph.ASTNodeKind) bool {
	switch kind {
	case graph.ASTNodeKindClass, graph.ASTNodeKindStruct, graph.ASTNodeKindInterface:
		return true
	}
	return false
}

// isConformanceTargetKind reports whether a target kind is an abstract type
// that can be implemented (interface/trait), as opposed to extended.
func isConformanceTargetKind(kind graph.ASTNodeKind) bool {
	switch kind {
	case graph.ASTNodeKindInterface, graph.ASTNodeKindTrait:
		return true
	}
	return false
}

// loadKnownNames builds a set of all distinct symbol names in the index for
// O(1) pre-filtering of references that resolve to nothing locally.
func (r *ReferenceResolver) loadKnownNames(ctx context.Context) (map[string]bool, error) {
	names, err := r.repo.ListASTNodeNames(ctx)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set, nil
}

// resolveReceiverType looks up a factory function/method by name and returns
// its inferred ReturnType (e.g. NewClient → "Client"), used to disambiguate
// chained calls like `svc := NewClient(); svc.Connect()`. Returns "" if the
// factory is unknown or has no return type. Uses the candidate cache to avoid
// re-querying across refs sharing the same factory.
func (r *ReferenceResolver) resolveReceiverType(ctx context.Context, factoryName string, cache map[string][]graph.ASTNode) (string, error) {
	candidates, ok := cache[factoryName]
	if !ok {
		c, err := r.repo.GetASTNodeByName(ctx, factoryName)
		if err != nil {
			return "", err
		}
		cache[factoryName] = c
		candidates = c
	}
	for i := range candidates {
		if candidates[i].ReturnType != "" {
			return candidates[i].ReturnType, nil
		}
	}
	return "", nil
}

// splitReference splits a possibly qualified reference name (e.g. "svc.Commit"
// or "fmt.Println") into the trailing lookup name and the qualifier prefix.
// Unqualified names return themselves with an empty qualifier.
func splitReference(refName string) (lookupName, qualifier string) {
	dot := strings.LastIndex(refName, ".")
	if dot < 0 {
		return refName, ""
	}
	return refName[dot+1:], refName[:dot]
}

// pickCandidate applies the disambiguation strategy:
//   - exactly one candidate → resolve
//   - qualifier present and exactly one candidate's package directory or
//     receiver type matches the qualifier → resolve to it
//   - otherwise, if exactly one candidate is exported → resolve to it
//   - else ambiguous (nil)
func pickCandidate(candidates []graph.ASTNode, qualifier string) *graph.ASTNode {
	switch len(candidates) {
	case 0:
		return nil
	case 1:
		return &candidates[0]
	}

	// When a qualifier is present (selector call), try to match it against
	// each candidate's package directory (e.g. "strings" in path "strings/...")
	// or the receiver type in its qualified name (e.g. "...::CommitService.Commit").
	if qualifier != "" {
		var match *graph.ASTNode
		for i := range candidates {
			c := &candidates[i]
			if receiverMatchesQualifier(c.QualifiedName, qualifier) ||
				pathMatchesQualifier(c.FilePath, qualifier) {
				if match != nil {
					return nil // still ambiguous
				}
				match = c
			}
		}
		if match != nil {
			return match
		}
	}

	var exported *graph.ASTNode
	for i := range candidates {
		if candidates[i].IsExported {
			if exported != nil {
				return nil
			}
			exported = &candidates[i]
		}
	}
	return exported
}

// receiverMatchesQualifier reports whether qualifier matches the receiver type
// in a qualified name of the form "<file>::<Type>.<method>" (possibly multiple
// "::"-delimited segments).
func receiverMatchesQualifier(qualifiedName, qualifier string) bool {
	if idx := strings.LastIndex(qualifiedName, "::"); idx >= 0 {
		qualifiedName = qualifiedName[idx+2:]
	}
	parts := strings.SplitN(qualifiedName, ".", 2)
	if len(parts) != 2 {
		return false
	}
	receiver := strings.TrimPrefix(parts[0], "*")
	return receiver == qualifier
}

// pathMatchesQualifier reports whether the qualifier matches the package name
// (the last path segment) of a file path, e.g. qualifier "strings" matches
// "strings/strings.go" or vendor path ".../strings/strings.go".
func pathMatchesQualifier(filePath, qualifier string) bool {
	if qualifier == "" {
		return false
	}
	dir := filePath
	if idx := strings.LastIndex(dir, "/"); idx >= 0 {
		dir = dir[:idx]
	}
	pkg := dir
	if idx := strings.LastIndex(pkg, "/"); idx >= 0 {
		pkg = pkg[idx+1:]
	} else {
		pkg = dir
	}
	return pkg == qualifier
}
