package application

import (
	"context"
	"strings"
	"time"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// IndexService orchestrates building the code knowledge graph from git history.
type IndexService struct {
	repo graph.GraphRepository
	git  graph.GraphGitClient
}

// NewIndexService creates an IndexService with the given repository and git client.
func NewIndexService(repo graph.GraphRepository, git graph.GraphGitClient) *IndexService {
	return &IndexService{repo: repo, git: git}
}

// FullIndex reads the complete git history and indexes all commits, authors,
// files, and relationships into the graph repository.
func (s *IndexService) FullIndex(ctx context.Context, req graph.IndexRequest) (*graph.IndexResult, error) {
	start := time.Now()

	maxFiles := req.MaxFilesPerCommit
	if maxFiles == 0 {
		maxFiles = 50
	}

	commits, err := s.git.CommitLogDetailed(ctx, "", req.MaxCommits)
	if err != nil {
		return nil, err
	}

	// git log returns newest-first; reverse for chronological processing.
	for i, j := 0, len(commits)-1; i < j; i, j = i+1, j-1 {
		commits[i], commits[j] = commits[j], commits[i]
	}

	var (
		indexedCommits int
		filesSeen      = make(map[string]bool)
		authorsSeen    = make(map[string]bool)
		lastHash       string
	)

	for _, ci := range commits {
		if maxFiles > 0 && len(ci.Files) > maxFiles {
			// Still count it as "seen" for lastHash tracking,
			// but skip the file-level indexing.
			lastHash = ci.Hash
			indexedCommits++
			if err := s.repo.UpsertCommit(ctx, commitNodeFrom(ci)); err != nil {
				return nil, err
			}
			if err := s.repo.UpsertAuthor(ctx, graph.AuthorNode{Email: ci.AuthorEmail, Name: ci.AuthorName}); err != nil {
				return nil, err
			}
			authorsSeen[ci.AuthorEmail] = true
			if err := s.repo.CreateAuthored(ctx, ci.AuthorEmail, ci.Hash); err != nil {
				return nil, err
			}
			continue
		}

		if err := s.repo.UpsertCommit(ctx, commitNodeFrom(ci)); err != nil {
			return nil, err
		}
		if err := s.repo.UpsertAuthor(ctx, graph.AuthorNode{Email: ci.AuthorEmail, Name: ci.AuthorName}); err != nil {
			return nil, err
		}
		authorsSeen[ci.AuthorEmail] = true
		if err := s.repo.CreateAuthored(ctx, ci.AuthorEmail, ci.Hash); err != nil {
			return nil, err
		}

		for _, fc := range ci.Files {
			filesSeen[fc.Path] = true
			if err := s.repo.UpsertFile(ctx, graph.FileNode{Path: fc.Path}); err != nil {
				return nil, err
			}
			if err := s.repo.CreateModifies(ctx, graph.ModifiesEdge{
				CommitHash: ci.Hash,
				FilePath:   fc.Path,
				Additions:  fc.Additions,
				Deletions:  fc.Deletions,
				Status:     fc.Status,
			}); err != nil {
				return nil, err
			}

			if strings.HasPrefix(fc.Status, "R") && fc.OldPath != "" {
				filesSeen[fc.OldPath] = true
				if err := s.repo.UpsertFile(ctx, graph.FileNode{Path: fc.OldPath}); err != nil {
					return nil, err
				}
				if err := s.repo.CreateRename(ctx, fc.OldPath, fc.Path, ci.Hash); err != nil {
					return nil, err
				}
			}
		}

		lastHash = ci.Hash
		indexedCommits++
	}

	if lastHash != "" {
		if err := s.repo.SetLastIndexedCommit(ctx, lastHash); err != nil {
			return nil, err
		}
	}

	// Run full co-change recompute after full index.
	minCount := 3
	if err := s.repo.RecomputeCoChanged(ctx, minCount, maxFiles); err != nil {
		return nil, err
	}

	return &graph.IndexResult{
		IndexedCommits: indexedCommits,
		NewCommits:     indexedCommits,
		Files:          len(filesSeen),
		Authors:        len(authorsSeen),
		DurationMs:     time.Since(start).Milliseconds(),
		LastCommit:     lastHash,
	}, nil
}

// IncrementalIndex indexes only commits after sinceHash up to HEAD.
func (s *IndexService) IncrementalIndex(ctx context.Context, sinceHash string, req graph.IndexRequest) (*graph.IndexResult, error) {
	start := time.Now()

	maxFiles := req.MaxFilesPerCommit
	if maxFiles == 0 {
		maxFiles = 50
	}

	commits, err := s.git.CommitLogDetailed(ctx, sinceHash, req.MaxCommits)
	if err != nil {
		return nil, err
	}

	// git log returns newest-first; reverse for chronological processing.
	for i, j := 0, len(commits)-1; i < j; i, j = i+1, j-1 {
		commits[i], commits[j] = commits[j], commits[i]
	}

	var (
		indexedCommits int
		filesSeen      = make(map[string]bool)
		authorsSeen    = make(map[string]bool)
		touchedFiles   []string
		lastHash       string
	)

	for _, ci := range commits {
		if maxFiles > 0 && len(ci.Files) > maxFiles {
			lastHash = ci.Hash
			indexedCommits++
			if err := s.repo.UpsertCommit(ctx, commitNodeFrom(ci)); err != nil {
				return nil, err
			}
			if err := s.repo.UpsertAuthor(ctx, graph.AuthorNode{Email: ci.AuthorEmail, Name: ci.AuthorName}); err != nil {
				return nil, err
			}
			authorsSeen[ci.AuthorEmail] = true
			if err := s.repo.CreateAuthored(ctx, ci.AuthorEmail, ci.Hash); err != nil {
				return nil, err
			}
			continue
		}

		if err := s.repo.UpsertCommit(ctx, commitNodeFrom(ci)); err != nil {
			return nil, err
		}
		if err := s.repo.UpsertAuthor(ctx, graph.AuthorNode{Email: ci.AuthorEmail, Name: ci.AuthorName}); err != nil {
			return nil, err
		}
		authorsSeen[ci.AuthorEmail] = true
		if err := s.repo.CreateAuthored(ctx, ci.AuthorEmail, ci.Hash); err != nil {
			return nil, err
		}

		for _, fc := range ci.Files {
			filesSeen[fc.Path] = true
			if err := s.repo.UpsertFile(ctx, graph.FileNode{Path: fc.Path}); err != nil {
				return nil, err
			}
			if err := s.repo.CreateModifies(ctx, graph.ModifiesEdge{
				CommitHash: ci.Hash,
				FilePath:   fc.Path,
				Additions:  fc.Additions,
				Deletions:  fc.Deletions,
				Status:     fc.Status,
			}); err != nil {
				return nil, err
			}

			if strings.HasPrefix(fc.Status, "R") && fc.OldPath != "" {
				filesSeen[fc.OldPath] = true
				if err := s.repo.UpsertFile(ctx, graph.FileNode{Path: fc.OldPath}); err != nil {
					return nil, err
				}
				if err := s.repo.CreateRename(ctx, fc.OldPath, fc.Path, ci.Hash); err != nil {
					return nil, err
				}
			}
		}

		lastHash = ci.Hash
		indexedCommits++
	}

	if lastHash != "" {
		if err := s.repo.SetLastIndexedCommit(ctx, lastHash); err != nil {
			return nil, err
		}
	}

	// Collect touched files for co-change computation.
	for f := range filesSeen {
		touchedFiles = append(touchedFiles, f)
	}

	// Run co-change computation for the incremental set.
	coChangeThreshold := req.CoChangeThreshold
	if coChangeThreshold == 0 {
		coChangeThreshold = 500
	}
	minCount := 3
	if len(touchedFiles) > 0 {
		if len(touchedFiles) > coChangeThreshold {
			if err := s.repo.RecomputeCoChanged(ctx, minCount, maxFiles); err != nil {
				return nil, err
			}
		} else {
			if err := s.repo.IncrementalCoChanged(ctx, touchedFiles, minCount, maxFiles); err != nil {
				return nil, err
			}
		}
	}

	return &graph.IndexResult{
		IndexedCommits: indexedCommits,
		NewCommits:     indexedCommits,
		Files:          len(filesSeen),
		Authors:        len(authorsSeen),
		DurationMs:     time.Since(start).Milliseconds(),
		LastCommit:     lastHash,
	}, nil
}

func commitNodeFrom(ci graph.CommitInfo) graph.CommitNode {
	return graph.CommitNode{
		Hash:         ci.Hash,
		Message:      ci.Message,
		AuthorName:   ci.AuthorName,
		AuthorEmail:  ci.AuthorEmail,
		Timestamp:    ci.Timestamp,
		ParentHashes: ci.ParentHashes,
	}
}
