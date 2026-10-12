// Package gogen generates Go type declarations from a JSON Schema, for
// [github.com/go-rotini/jsonschema].
//
//	src, err := gogen.Generate(schemaJSON, gogen.WithPackage("models"))
//
// The output is gofmt-formatted source: a struct for the schema's root
// object plus a named type for every entry under "definitions" / "$defs".
// A $ref to such an entry (in this document or, by the same name, in
// another) uses that type, and "#" uses the root type. Types are generated
// for this document only, so any other $ref, such as one to a whole remote
// document, maps to any.
//
// The command `go tool jsonschema generate` wraps it for go:generate use.
package gogen
