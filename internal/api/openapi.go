package api

import _ "embed"

//go:embed docs/swagger.json
var openapiSpec []byte
