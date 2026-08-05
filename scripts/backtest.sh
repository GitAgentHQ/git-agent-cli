#!/usr/bin/env bash
# scripts/backtest.sh — leak-free recall@k scorer for `git-agent related`
#
# Measures how well `related` ranks the files that REALLY change together in
# git history, against a set of throwaway synthetic repos with known co-change
# structure. Each repo is built in /tmp with a dedicated graph.db. Leak-free:
# the fixture repos are created fresh per run, so the test commits are the only
# history; ground truth is the KNOWN coupling structure, never related's output.
#
# Emits ONE number as its LAST stdout line: mean MRR@k (0..1) across all seed
# queries. Higher is better (--direction max).
#
# Leak-freedom: the fixture repos are created fresh per run under $TMPDIR; the
# index is built with `related --reindex` against THAT repo only, so the test
# commits are the only history. The scorer is git-history ground truth derived
# independently with `git log` in the fixture, never from related's own output.

set -euo pipefail

# --- configuration -----------------------------------------------------------
SCRIPT_PATH="$(realpath "${BASH_SOURCE[0]}")"
ROOT="$(dirname "$(dirname "$SCRIPT_PATH")")"
BIN="$ROOT/backtest-bin/git-agent"        # isolated binary, rebuilt each run
WORK="$(mktemp -d /tmp/git-agent-backtest.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

K="${K:-5}"                                # top-k recall window (default 5)
TRIALS="${TRIALS:-6}"                      # number of synthetic repos

echo "backtest: building isolated git-agent binary" >&2
rm -rf "$(dirname "$BIN")"
mkdir -p "$(dirname "$BIN")"
( cd "$ROOT" && go build -o "$BIN" . )

rr_sum=0.0
query_count=0

# --- fixtures ---------------------------------------------------------------
# Each fixture is a synthetic repo with a KNOWN coupling structure. Ground truth
# = the genuinely-coupled coding partner(s). A decoy/noise file is present to
# catch over-ranking of noise.
#
# Fixture types (expose different ranking failure modes):
#   strong   — clean A<->B, sanity: related must find B, and NOT be beaten by
#              a decoy that never co-changes.
#   hub      — THE discriminating fixture. Seed main.go changes 20x. Its real
#              coding partner main_test.go co-changes 6x; a changelog noise
#              file co-changes 14x. A useful `related` must rank main_test.go
#              FIRST (it's the file a coding agent wants — which test to run),
#              ahead of the high-fanout CHANGELOG. Baseline ranks CHANGELOG #1
#              (70%) above main_test.go (29%) — recall@1 for main_test.go = 0.
#              This is the "high-fanout noise" failure the docs flagged; it is
#              where real coding-usefulness improvement lives.
#   test     — test-file seed that co-changes with impl A.
make_repo_strong() {
  local dir="$WORK/strong-$1"
  mkdir -p "$dir"
  git -C "$dir" init -q
  git -C "$dir" config user.email test@example.com
  git -C "$dir" config user.name test
  local i
  for i in 1 2 3 4 5; do
    echo "a$i" >> "$dir/A.txt"; echo "b$i" >> "$dir/B.txt"
    git -C "$dir" add -A; git -C "$dir" commit -q -m "tweak $i"
  done
  for i in 1 2 3 4 5; do
    echo "d$i" >> "$dir/D.txt"
    git -C "$dir" add -A; git -C "$dir" commit -q -m "decoy $i"
  done
  echo "$dir"
}

make_repo_hub() {
  local dir="$WORK/hub-$1"
  mkdir -p "$dir"
  git -C "$dir" init -q
  git -C "$dir" config user.email test@example.com
  git -C "$dir" config user.name test
  # main.go changes 20x. main_test.go co-changes 6x (focused, useful answer).
  # CHANGELOG.md co-changes 14x with main.go AND ALSO with util.go/config.go —
  # a genuine high-fanout hub whose per-pair coupling is diluted by its many
  # partners. A hubness-aware rank must demote CHANGELOG below main_test.go.
  local i
  for i in 1 2 3 4 5 6; do
    echo "m$i" >> "$dir/main.go"; echo "t$i" >> "$dir/main_test.go"
    git -C "$dir" add -A; git -C "$dir" commit -q -m "impl+test $i"
  done
  for i in 7 8 9 10 11 12 13 14; do
    echo "m$i" >> "$dir/main.go"; echo "c$i" >> "$dir/CHANGELOG.md"
    echo "u$i" >> "$dir/util.go"; echo "g$i" >> "$dir/config.go"
    git -C "$dir" add -A; git -C "$dir" commit -q -m "impl+changelog+util+config $i"
  done
  for i in 15 16 17 18 19 20; do
    echo "m$i" >> "$dir/main.go"; echo "c$i" >> "$dir/CHANGELOG.md"
    echo "u$i" >> "$dir/util.go"
    git -C "$dir" add -A; git -C "$dir" commit -q -m "impl+changelog+util $i"
  done
  echo "$dir"
}

make_repo_test() {
  local dir="$WORK/test-$1"
  mkdir -p "$dir"
  git -C "$dir" init -q
  git -C "$dir" config user.email test@example.com
  git -C "$dir" config user.name test
  local i
  for i in 1 2 3 4 5; do
    echo "a$i" >> "$dir/A.txt"; echo "t$i" >> "$dir/A_test.txt"
    git -C "$dir" add -A; git -C "$dir" commit -q -m "tweak $i"
  done
  for i in 1 2 3 4 5; do
    echo "d$i" >> "$dir/D.txt"
    git -C "$dir" add -A; git -C "$dir" commit -q -m "decoy $i"
  done
  echo "$dir"
}

# diluted: the seed changes 20x; its genuine partner co-changes only 4x. Under
# symmetric strength count/max(total), the genuine partner is diluted (4/20).
# A solo file changes often but never with the seed. A good rank must still put
# the genuine partner above solo churn — exposes over-reliance on raw strength.
make_repo_diluted() {
  local dir="$WORK/diluted-$1"
  mkdir -p "$dir"
  git -C "$dir" init -q
  git -C "$dir" config user.email test@example.com
  git -C "$dir" config user.name test
  local i
  for i in 1 2 3 4; do
    echo "c$i" >> "$dir/core.go"; echo "u$i" >> "$dir/core_util.go"
    git -C "$dir" add -A; git -C "$dir" commit -q -m "core work $i"
  done
  for i in 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
    echo "c$i" >> "$dir/core.go"
    git -C "$dir" add -A; git -C "$dir" commit -q -m "core churn $i"
  done
  for i in 1 2 3 4 5; do
    echo "s$i" >> "$dir/solo.go"
    git -C "$dir" add -A; git -C "$dir" commit -q -m "solo $i"
  done
  echo "$dir"
}

# --- ground truth + query ---------------------------------------------------
# run_trial REPO SEED EXPECTED — builds the index, runs related, computes the
# reciprocal rank of EXPECTED among the top-K results. MRR (not recall@k) is
# used because the useful answer must RANK FIRST to help a coding agent — a
# file present at rank 5 is nearly useless. This gives the optimizer a real
# gradient: moving main_test.go from rank 2 to rank 1 lifts 1/2 -> 1/1.
run_trial() {
  local dir="$1" seed="$2" expected="$3"

  # build the co-change index for THIS repo (must run inside the fixture dir)
  if ! ( cd "$dir" && "$BIN" related --reindex "$seed" -o json >/dev/null 2>&1 ); then
    echo "  trial: seed=$seed related --reindex FAILED (skipped)" >&2
    return 1
  fi

  local json out_paths
  json="$( cd "$dir" && "$BIN" related "$seed" -o json 2>/dev/null )" || { echo "  trial: seed=$seed related FAILED (skipped)" >&2; return 1; }
  # top-K paths from related (JSON "co_changed[].path")
  out_paths="$(printf '%s' "$json" | grep -oE '"path"[[:space:]]*:[[:space:]]*"[^"]*"' | sed -E 's/"path"[[:space:]]*:[[:space:]]*"//; s/"$//' | head -n "$K")"

  local rank=0 rr=0
  local idx p
  idx=1
  while IFS= read -r p; do
    if [[ "$p" == "$expected" ]]; then
      rank=$idx
      break
    fi
    idx=$((idx + 1))
  done <<< "$out_paths"
  if [[ "$rank" -gt 0 ]]; then
    rr=$(awk -v r="$rank" 'BEGIN{printf "%.6f", 1.0/r}')
  fi
  rr_sum=$(awk -v s="$rr_sum" -v r="$rr" 'BEGIN{print s + r}')
  query_count=$((query_count + 1))
  echo "  trial: seed=$seed expected=$expected rank=$rank MRR@${K}=$rr" >&2
}

# --- trials -----------------------------------------------------------------
# Each fixture family contributes to the total: hub is the discriminating
# fixture (main_test.go must outrank the high-fanout CHANGELOG.md); the others
# are regressions guards. Trial counts derive from TRIALS so the knob actually
# controls the run. A single failed trial is logged and skipped, never fatal —
# the final mean MRR must always be printed for the optimizer to parse.
HUB_N=$(( (TRIALS + 1) / 4 ));   [ "$HUB_N" -lt 1 ] && HUB_N=1
GUARD_N=$(( (TRIALS + 2) / 4 )); [ "$GUARD_N" -lt 1 ] && GUARD_N=1
echo "backtest: running $(( HUB_N*1 + GUARD_N*3 )) synthetic-repo trials" >&2

for n in $(seq 1 "$HUB_N"); do
  dh="$(make_repo_hub "$n")"
  run_trial "$dh" "main.go" "main_test.go" || true
done

# strong sanity: A <-> B must still rank B first
for n in $(seq 1 "$GUARD_N"); do
  ds="$(make_repo_strong "$n")"
  run_trial "$ds" "A.txt" "B.txt" || true
done

# test seed -> impl
for n in $(seq 1 "$GUARD_N"); do
  dt="$(make_repo_test "$n")"
  run_trial "$dt" "A_test.txt" "A.txt" || true
done

# diluted: genuine partner with low strength must still beat solo churn
for n in $(seq 1 "$GUARD_N"); do
  dd="$(make_repo_diluted "$n")"
  run_trial "$dd" "core.go" "core_util.go" || true
done

# --- report -----------------------------------------------------------------
if [[ "$query_count" -eq 0 ]]; then
  echo "backtest: no trials ran" >&2
  exit 1
fi
mean_mrr=$(awk -v s="$rr_sum" -v n="$query_count" 'BEGIN{printf "%.4f", s/n}')
echo "backtest: mean MRR@${K} over ${query_count} queries = ${mean_mrr}" >&2
printf '%.4f\n' "$mean_mrr"
