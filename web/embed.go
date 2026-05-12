package webassets

import "embed"

// Static contains the browser UI served by the compiled binary.
//
//go:embed static
var Static embed.FS
