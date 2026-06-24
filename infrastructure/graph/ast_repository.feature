Feature: AST Node and Edge Storage

  The structural code intelligence layer stores AST-extracted symbols and their
  relationships in SQLite, so that callers/callees/impact queries can traverse
  the code structure graph.

  Background:
    Given an open graph database with AST schema initialized

  Scenario: Upsert and retrieve an AST node by name
    Given an ASTNode with kind "function" and name "HandleRequest" is upserted
    When the node is searched by name "HandleRequest"
    Then a matching node with kind "function" is returned

  Scenario: Upsert and retrieve an AST node by qualified_name
    Given an ASTNode with qualified_name "handler.go::HandleRequest" is upserted
    When the node is retrieved by qualified_name "handler.go::HandleRequest"
    Then the node is returned with the correct name

  Scenario: Upsert AST edges and query callers
    Given ASTNodes "run" and "process" are stored
    And a calls edge from "run" to "process" is upserted
    When callers of "process" are queried with depth 1
    Then "run" appears in the callers result

  Scenario: Upsert AST edges and query callees
    Given ASTNodes "run" and "process" are stored
    And a calls edge from "run" to "process" is upserted
    When callees of "run" are queried with depth 1
    Then "process" appears in the callees result

  Scenario: Impact radius via BFS on incoming edges
    Given a chain of calls edges A→B→C→D
    When impact radius is queried from "D" with depth 3
    Then C, B, and A all appear in the impact result
    And each entry has the correct depth (C=1, B=2, A=3)

  Scenario: Symbol search finds nodes by name prefix
    Given multiple ASTNodes are stored including "HandleRequest" and "HandleError"
    When a search query "Handle" is executed
    Then both "HandleRequest" and "HandleError" are in the results

  Scenario: Upsert unresolved refs
    Given an ASTNode "run" and an unresolved ref for "Println"
    When the unresolved ref is upserted
    Then it can be retrieved by reference_name "Println"

  Scenario: Delete AST nodes for a file removes all its symbols
    Given ASTNodes for "handler.go" including "HandleRequest" and "process"
    When all ASTNodes for "handler.go" are deleted
    Then searching "HandleRequest" returns no results
    And the file node for "handler.go" is also removed
