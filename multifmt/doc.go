// Package multifmt adapts the sister go-rotini format packages
// ([go-rotini/jsonc], [go-rotini/yaml], [go-rotini/toml]) so JSON Schema
// schemas and instances can be loaded from JSONC, YAML, or TOML in addition
// to plain JSON. It is an opt-in sub-package: callers who only need JSON
// schemas/instances do not pull in the non-JSON parsers.
//
// Implementation lands in Phase 8 of the package roadmap.
//
// [go-rotini/jsonc]: https://github.com/go-rotini/jsonc
// [go-rotini/yaml]: https://github.com/go-rotini/yaml
// [go-rotini/toml]: https://github.com/go-rotini/toml
package multifmt
