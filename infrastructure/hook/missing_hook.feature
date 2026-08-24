Feature: Configured shell hook failures

  Scenario: Missing configured hook blocks execution
    Given a configured hook path does not exist
    When the hook executor runs that hook
    Then execution returns an error
    And the commit is not silently allowed
