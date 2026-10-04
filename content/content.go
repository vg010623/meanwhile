// Package content embeds the curriculum files that ship inside the binary, so
// lessons can never go missing at runtime.
package content

import "embed"

// FS holds the concept files. Concepts live under Dir.
//
//go:embed concepts/*.md
var FS embed.FS

// Dir is the directory inside FS that holds the concept files.
const Dir = "concepts"
