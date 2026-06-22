package extraction

type LanguageExtractor struct {
	FunctionTypes      []string
	MethodTypes        []string
	ClassTypes         []string
	InterfaceTypes     []string
	StructTypes        []string
	EnumTypes          []string
	TypeAliasTypes     []string
	ImportTypes        []string
	CallTypes          []string
	VariableTypes      []string
	NameField          string
	BodyField          string
	MethodsAreTopLevel bool
}
