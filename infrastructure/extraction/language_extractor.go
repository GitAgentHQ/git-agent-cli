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
	FieldTypes         []string
	NameField          string
	BodyField          string
	MethodsAreTopLevel bool
}
