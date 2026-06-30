Feature: Related Command Output

  The `related` text output must not let a transitively-reached file be mistaken
  for one that changes directly with the target: the percentage shown is the
  coupling strength of the last hop, not of a direct target-to-file link. Each
  related file also carries the commits that link it to the seeds, so an agent
  sees not just what changes together but why.

  Scenario: A direct coupling is shown without a depth marker
    Given a co-changed file reached at depth 1
    When related renders the text output
    Then its line shows the strength and co-change count with no depth marker

  Scenario: A transitive coupling is marked with its depth
    Given a co-changed file reached at depth 2
    When related renders the text output
    Then its line is annotated "[indirect, depth 2]"

  Scenario: Linking commits are shown as evidence
    Given a co-changed file linked to the seed by commits about "auth"
    When related renders the text output
    Then each linking commit's short sha and subject are listed under the file

  Scenario: Linking commits are emitted as structured JSON
    Given a co-changed file linked to the seed by one or more commits
    When related renders the JSON output
    Then the file entry carries a "commits" array of {sha, subject, ts}
