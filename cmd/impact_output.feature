Feature: Impact Command Output

  The impact text output must not let a transitively-reached file be mistaken
  for one that changes directly with the target: the percentage shown is the
  coupling strength of the last hop, not of a direct target-to-file link.

  Scenario: A direct coupling is shown without a depth marker
    Given a co-changed file reached at depth 1
    When impact renders the text output
    Then its line shows the strength and co-change count with no depth marker

  Scenario: A transitive coupling is marked with its depth
    Given a co-changed file reached at depth 2
    When impact renders the text output
    Then its line is annotated "[indirect, depth 2]"
