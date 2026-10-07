In the repository /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli, on a new branch fix/staged-boundary cut from develop, git-agent stops destroying the split boundary the user expressed in the index, with no region inference anywhere:

1. A run whose index holds changes commits exactly that content as its first commit, from the diff the index holds, without unstaging, restaging, rebuilding content, or writing an index entry itself. The reproduced develop defect stops: with only the func Beta region staged by hand and the func Epsilon change loose, the first commit carries Beta alone and Epsilon stays in the working tree.
2. `--no-stage` commits the index and nothing else, which is what its help text claims.
3. After that commit the remaining changes follow develop's existing file-level flow unchanged, so a run whose index is empty behaves exactly as develop: same prompts, exit code, commit subjects, and JSON envelope.
4. A dry run with staged content previews the same commits a real run would make and gives the index back unchanged, without creating a commit.
5. The staged diff passes through the same content filter and the same line and byte caps as every other diff the model sees.
6. When a file the run is about to commit whole holds two or more git-anchored regions, the run reports that on a stream a scripted run receives, naming the regions and `git add -p <file>`. It reports and never splits on its own, and it infers no boundary.
7. The change is a few hundred production lines, adds no tunable setting, and carries named tests: the develop-binary defect reproduction, the `--no-stage` index-only case, the dry-run preview and index restore, the report and its absence, and the empty-index parity comparison.
8. develop itself is unmodified; the work lives on the branch, and the branch tip and how to merge or remove it are reported.
