Feature: Reporting files with multiple diff regions

  The index is the only split git-agent acts on. It reports multi-region files
  without staging or inferring a boundary. A large working tree must not turn
  that report into one warning line per file.

  Scenario: A few multi-region files are each named on the warning stream
    Given a remainder diff with two files that each hold two regions
    When the run reports splittable files without --verbose
    Then the warning stream names both files and their git add -p commands

  Scenario: Exactly three multi-region files are still named individually
    Given a remainder diff with three files that each hold two regions
    When the run reports splittable files without --verbose
    Then the warning stream names all three files and their git add -p commands
    And it contains no bounded-summary hint

  Scenario: Many multi-region files produce one bounded warning
    Given a remainder diff with four files that each hold two regions
    When the run reports splittable files without --verbose
    Then the warning stream contains exactly one line
    And it states that four files hold separate regions
    And it names a concrete git add -p command
    And it directs the user to --verbose for the full list

  Scenario: --verbose carries every per-file detail
    Given a remainder diff with four files that each hold two regions
    When the run reports splittable files with --verbose
    Then the warning stream still contains one bounded line
    And it does not repeat the --verbose hint
    And the bounded line appears before the per-file details
    And the verbose stream names all four files and their git add -p commands
