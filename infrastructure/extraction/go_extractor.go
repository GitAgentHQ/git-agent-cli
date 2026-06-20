package extraction

func GoExtractor() *LanguageExtractor {
	return &LanguageExtractor{
		FunctionTypes:      []string{"function_declaration"},
		MethodTypes:        []string{"method_declaration"},
		ClassTypes:         []string{},
		InterfaceTypes:     []string{},
		StructTypes:        []string{},
		EnumTypes:          []string{},
		TypeAliasTypes:     []string{"type_spec"},
		ImportTypes:        []string{"import_declaration"},
		CallTypes:          []string{"call_expression"},
		VariableTypes:      []string{"var_declaration", "short_var_declaration", "const_declaration"},
		NameField:          "name",
		BodyField:          "body",
		ParamsField:        "parameters",
		ReturnField:        "result",
		MethodsAreTopLevel: true,
	}
}
