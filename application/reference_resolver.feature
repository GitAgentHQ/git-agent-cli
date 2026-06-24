Feature: Cross-file reference resolution

  After AST indexing, same-file calls are resolved into ast_edges, but cross-file
  calls remain as ast_unresolved_refs. The ReferenceResolver reads all unresolved
  refs and matches them against the global symbol table to create resolved edges.

  Background:
    Given an AST repository with indexed symbols and unresolved refs

  Scenario: Unambiguous reference resolves to an edge
    When the resolver processes an unresolved ref whose name matches exactly one symbol
    Then a calls edge is created from the source node to the matched symbol
    And the resolution count is incremented

  Scenario: Ambiguous reference with one exported symbol resolves to that symbol
    When the resolver processes an unresolved ref whose name matches multiple symbols
    But exactly one of them is exported
    Then a calls edge is created to the exported symbol

  Scenario: Ambiguous reference with no exported symbol is left unresolved
    When the resolver processes an unresolved ref whose name matches multiple non-exported symbols
    Then no edge is created
    And the ambiguous count is incremented

  Scenario: Reference with no match remains unresolved
    When the resolver processes an unresolved ref whose name matches zero symbols
    Then no edge is created
    And the not-found count is incremented

  Scenario: Resolver runs after full index and resolves cross-file calls
    Given two files where file A calls a function defined in file B
    When the resolver runs
    Then the cross-file call edge exists in the graph
