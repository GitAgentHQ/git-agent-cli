package hooks

import _ "embed"

//go:embed empty.sh
var Empty []byte

//go:embed conventional.sh
var Conventional []byte

//go:embed shim.sh
var Shim []byte
