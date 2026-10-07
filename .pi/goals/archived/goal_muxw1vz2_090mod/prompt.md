In the worktree /Users/FradSer/Developer/FradSer/git-agent/git-agent-hunks on branch feat/hunk-staging, the hunk-splitting logic is corrected where it was unprincipled, and the remaining boundaries are documented as boundaries:

1. Region inventory. The regions come from a context width the tool chooses, not from git's default merge. A change that default git folds into another hunk becomes a region of its own, and applying every region still reproduces the working tree content exactly.
2. Region independence. Two edits split when they are separated, including two edits inside one long function; they stay one region when the code between them is what makes them one edit. The rule is positional, not a guess about the code's meaning, and it is a single documented number the user can raise.
3. The auto-mode size floor is a documented config key with a stated default, not an unnamed constant inside the splitter.
4. A split the run's commit cap skips is reported to the user on the always-visible stream, not only in the verbose log.
5. With split_hunks unset, every existing behavior, prompt, exit code, and JSON field stays byte-identical to develop.
6. The two boundaries that stay: nothing checks that a region commit builds on its own, and the model does not choose the region grouping. Both are stated in the README and the served skill docs, together with the new keys.

Every changed rule carries a regression test that fails against the previous implementation.
