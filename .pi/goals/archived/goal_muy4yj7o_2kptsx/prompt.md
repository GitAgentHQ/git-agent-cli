In a fresh worktree /Users/FradSer/Developer/FradSer/git-agent/git-agent-intent on a new branch feat/staged-boundaries cut from develop, git-agent acts only on the split boundary the user has already expressed in the index, and reports when a file could be split, instead of inferring regions.

What must be true when this is met:

1. A run whose index already holds changes commits exactly the index content as its first commit, from the diff the index holds, without unstaging or re-staging anything. The defect reproduced on develop — a hand-staged region followed by `git commit --no-stage` puts unstaged work into the commit — does not happen. The unstaged remainder of the same file stays out of that commit.
2. `--no-stage` commits exactly what the index holds and nothing else, which is what its own help text already claims.
3. After that first commit, the remaining changes follow the existing file-level flow unchanged, so a run with an empty index behaves exactly as develop does.
4. When a changed file's own diff covers two or more git-anchored regions, the run reports that fact and names the regions and the command that splits them (`git add -p`), on the warning stream a scripted run also receives. No region is inferred, no content is rebuilt, and no index entry is written by git-agent itself.
5. No region policy remains: there is no gap, no size floor, no hunk inventory, no applier, no ledger, and nothing left to tune or measure. The production code this feature adds is a few hundred lines at most, and the model-free split measurement harness is not needed by it.
6. The working tree is never written to, and a preview (`--dry-run`) shows the same commits without committing.
7. Whole-file behavior for a run whose index is empty is byte-identical to develop: same prompts, same exit codes, same JSON fields, same commit content.

The branch carries a written comparison against feat/hunk-staging: production lines added, the behavior difference on the reproduced case, the risk each approach carries, and what the earlier measurement says about each.
