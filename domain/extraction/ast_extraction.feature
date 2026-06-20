Feature: AST Extraction from Source Files

  A code intelligence layer should parse source files with tree-sitter, extract
  code symbols (functions, classes, methods, structs, interfaces, etc.) and
  their relationships (calls, contains, references), and store them so that
  structural impact queries can answer "what code does this function touch?"

  Background:
    Given a Go source file "handler.go" in the project

  Scenario: Extract Go function declarations
    Given "handler.go" contains `func HandleRequest() error { return nil }`
    When the extractor parses "handler.go"
    Then an ASTNode with kind "function" and name "HandleRequest" is created
    And the node's qualified_name includes "HandleRequest"
    And the node has start_line and end_line

  Scenario: Extract Go method declarations with receiver
    Given "handler.go" contains `func (s *Server) Start() { s.run() }`
    When the extractor parses "handler.go"
    Then an ASTNode with kind "method" and name "Start" is created
    And the qualified_name includes the receiver type "Server"

  Scenario: Extract Go struct via type_spec
    Given "handler.go" contains `type Config struct { Port int }`
    When the extractor parses "handler.go"
    Then an ASTNode with kind "struct" and name "Config" is created

  Scenario: Extract Go interface via type_spec
    Given "handler.go" contains `type Handler interface { Serve() error }`
    When the extractor parses "handler.go"
    Then an ASTNode with kind "interface" and name "Handler" is created

  Scenario: Extract call edges from function body
    Given "handler.go" contains a function "run" that calls "process" and "log"
    When the extractor parses "handler.go"
    Then ASTEdge entries with kind "calls" link "run" to "process" and "log"
    And each call edge has a line number

  Scenario: Extract contains edges from file to symbols
    Given "handler.go" declares two functions
    When the extractor parses "handler.go"
    Then ASTEdge entries with kind "contains" link the file node to each function

  Scenario: Extract import declarations
    Given "handler.go" contains `import "fmt"`
    When the extractor parses "handler.go"
    Then an ASTNode with kind "import" and name "fmt" is created
    And an ASTEdge with kind "imports" links the file node to the import node

  Scenario: Extract Go exported status
    Given "handler.go" contains `func PublicFunc() {}` and `func privateFunc() {}`
    When the extractor parses "handler.go"
    Then "PublicFunc" has is_exported true
    And "privateFunc" has is_exported false

  Scenario: Extract function signatures
    Given "handler.go" contains `func Add(a int, b int) int`
    When the extractor parses "handler.go"
    Then the "Add" node has signature "(a int, b int) int"

  Scenario: Unresolved refs for external calls
    Given "handler.go" contains `func run() { fmt.Println("hello") }`
    When the extractor parses "handler.go"
    Then an unresolved ref for "Println" is created
    And the unresolved ref has reference_kind "calls"

  Scenario: A file with no extractable symbols produces empty result
    Given "handler.go" is an empty file with only a package declaration
    When the extractor parses "handler.go"
    Then the result contains only a file node
    And no symbol nodes are created
