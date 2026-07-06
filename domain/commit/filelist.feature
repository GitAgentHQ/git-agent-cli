Feature: File list summarization for the planner prompt

  A commit that touches thousands of files (e.g. untracking a vendored
  dependency directory) must not send one prompt line per file to the LLM
  planner — that burns tokens the planner does not need and can stall a
  small/cheap model outright. SummarizeFileList collapses an oversized file
  list into directory-level summaries, rolling up one path level at a time,
  while an exact-sized list passes through unchanged.

  Scenario: A file list within the budget passes through unchanged
    Given 5 files and a budget of 150
    When the file list is summarized
    Then the labels equal the original files verbatim
    And there is no expansion map

  Scenario: An oversized flat directory collapses to one summary label
    Given 2000 files all under "vendor/lib/" and a budget of 150
    When the file list is summarized
    Then there is exactly 1 label
    And the label reads "vendor/lib/ (2000 files)"
    And expanding that label returns all 2000 original files

  Scenario: Collapsing rolls up only as many levels as needed
    Given 3 files under "src/app/" and 200 files under "vendor/lib/" with a budget of 150
    When the file list is summarized
    Then the 3 "src/app/" files remain listed individually
    And the 200 "vendor/lib/" files collapse to one summary label

  Scenario: A fully flat list with no shared subdirectory collapses to the repository root
    Given 500 distinct single-segment root files and a budget of 150
    When the file list is summarized
    Then there is exactly 1 label
    And the label reads "./ (500 files)"
