---
name: jev-owns-decisions-model-owns-prose
description: the optional TypeSafe System One layer decides, the generative provider only writes; Jev never filters model output
type: decision
---

The user directed (2026-10) that git-agent uses Jev (TypeSafe System One) as a
model plus a judgment layer, with Jev as an optional enhancement. The split is
fixed by capability, not preference: Jev returns typed choices and no text, so
it owns which-scope-covers-which-directory, and the generative provider owns
every wording task, including the scope descriptions.

Jev is not a reranker over model output. A first attempt asked the text model
for candidates and let Jev pick one; that was rejected. A decision comes before
any generation, and code supplies the whole candidate set so the model can only
select among options code already holds.

The layer is off unless `jev_api_key` is set or `TYPESAFE_API_KEY` is exported.
A nil decider is the documented off switch, and a decider failure falls back to
the text model with one verbose line, so a TypeSafe outage cannot block a commit.

Measured on 12 real commits of this repository (2026-10, jev-1.13.0): one
combined Choice over type x scope scored 8/12 on the type and 4/12 jointly;
splitting it into an atomic type Choice, a scope Choice, and a Score scored 9/12
on the type and 8/12 jointly at 1.2-3.6 s per call. A scope decision over 18
candidate directories scored 9/9 against this repository's own configured
scopes, all with confidence at or above 0.72, in one 2.5 s request.

Two further findings shaped the design. First, the commit planner's text output
is discarded: `commitGroups` regenerates every group message, so the planner's
only durable product is the file grouping, which is a decision rather than a
generation. Second, the judgments do not need the diff body. Replacing it with
paths, line counts, and touched symbols cost 8/12 on the type and 11/12 on the
scope at 38 percent fewer tokens, so the diff never has to reach a second
provider.

The full seam measurement, run through `infrastructure/eval`, is the honest
picture and it is uneven. The scope seam is ready: 8 directories judged in one
request, every assignment correct, confidence 0.75, above the 0.7 floor.

The commit prefix seam splits in two, because its two answers are not equally
trustworthy. Over several runs the scope agreed in 92 percent of cases and every
disagreement arrived around 0.45 to 0.53 confidence, so a 0.7 floor separates it
cleanly and a confident scope may be pinned. The type agreed in 58 to 67 percent
of cases and its disagreements arrived at 0.81 to 0.94 confidence: the wrong
answers are the confident ones. No floor separates right from wrong, so the type
has no floor at all and is never pinned.

That last point was nearly shipped wrong. A 0.85 type floor looked conservative on
paper, and a clean-environment run showed the layer pinning a type at 0.95 while
the measurement said wrong types arrive at 0.94. A threshold fitted to look safe is
not safe. The harness now reports the confidence each disagreement happened at and
asserts the property a floor depends on, so the claim is checked rather than
asserted.

**Why**: a text-generation model coerced into structured decisions needs parsing
and cannot report how sure it is; a System One model answers exactly that
question. Keeping Jev off the prose path is what makes the layer an enhancement
instead of a replacement.

Two bugs came out of running the real binary rather than the unit tests. Both
were shared state that a value copy silently split: the per-run call budget
lived in a copied `Policy`, and the per-seam miss counter lived in a fresh
session map, so neither accumulated. A rule that must hold across a whole run has
to live behind a pointer the run owns.

The A/B settled it. Running both sides on the same real commits, with a real
generative provider as the baseline, gave this picture over three repositories:
the type judgment scored 21 of 32 against 26 of 32 for the baseline, so it is
consistently worse than the model it would replace. The scope judgment was
inside the noise of a single nine-sample repository. The grouping judgment
disagreed with the baseline grouping in roughly four cases out of five, and every
hand-adjudicated case went the same way: it over-splits.

The over-splitting has a cause in the design rather than in the model. Its
candidate set is the top-level directory, so it can only merge or split at that
granularity, and its evidence is paths and line counts with no diff. A change
that spans five directories as one feature looks to it like five commits. The
baseline sees the whole change and can say one.

So the layer earns nothing in the commit flow. It stays available and it stays
measured, but every seam that would have replaced a working decision is now
shadow-only. Keep it only for the per-directory scope judgment, which decided
this repository's own scopes correctly and costs one request.

**How to apply**: measure a seam against the behavior it replaces, not against
the human title alone, before opening its mode. A judgment scored against the
wrong baseline is worse than no measurement. When adding a seam, decide first whether the answer is a judgment or a
wording. Put the judgment behind a `domain` port with a typed
answer, build candidates in code, and let the existing provider write text.
Measure the seam against real repository history before claiming it works. Add
new config keys to `infrastructure/config/keys.go` and keep the flag policy in
`git-agent-cli/AGENTS.md`: credentials and models are config keys, not flags.
TypeSafe ships no Go SDK, so `infrastructure/typesafe` stays on net/http to keep
release builds cgo-free.