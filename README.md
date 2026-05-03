# go-rotini/jsonschema

A Go [JSON Schema](https://json-schema.org/) package that compiles, validates, and generates schemas, with multi-format input (JSON, JSONC, YAML, TOML) and the same operational ergonomics — typed errors, functional options, source-pointer error formatting — as the rest of the rotini package family.

This package is used as the default JSON Schema support package for [rotini](https://github.com/go-rotini/rotini).

## Features

- Full [Draft 2020-12](https://json-schema.org/draft/2020-12/schema) support, plus read-only support for Draft 2019-09, Draft 7, Draft 6, and Draft 4
- Compile/validate split (`Compile` → `*Schema` → `Validate`) so compilation amortizes across many validations
- All four output formats from Draft 2020-12 §12: Flag, Basic, Detailed, Verbose
- Generic `ValidateTo[T]` typed-decode in one call
- Schema generation from Go types via reflection (`Generate`, `GenerateBytes`, `FromType`)
- Multi-format instance and schema input (JSONC, YAML, TOML) via `LoadJSONC` / `LoadYAML` / `LoadTOML` and their `Validate*` counterparts
- `$ref` and `$dynamicRef` resolution; pluggable `Loader` (HTTPS-only by default; opt-in HTTP / file)
- Custom keywords and vocabularies via `WithVocabulary`
- Built-in format validators: `date-time`, `date`, `time`, `duration`, `email`, `idn-email`, `hostname`, `idn-hostname`, `ipv4`, `ipv6`, `uri`, `uri-reference`, `iri`, `iri-reference`, `uri-template`, `json-pointer`, `relative-json-pointer`, `uuid`, `regex`
- Content vocabulary: `contentEncoding`, `contentMediaType`, `contentSchema` (annotation-only by default)
- Two strict modes: `WithMetaSchemaValidation` (compile-time meta-schema check) and `WithFormatAssertion` (runtime format assertion)
- `RenderError` for human-readable validation errors with a source pointer into the instance
- DoS protection: max ref depth, max recursion depth, max document size, ref-loop detection
- Sister-package format support (JSONC / YAML / TOML) provided by [go-rotini/jsonc](https://github.com/go-rotini/jsonc), [go-rotini/yaml](https://github.com/go-rotini/yaml), and [go-rotini/toml](https://github.com/go-rotini/toml)

## Installation

```bash
go get github.com/go-rotini/jsonschema
```

Requires Go 1.23 or later.

## Quick Start

```go
package main

import (
	"fmt"
	"log"

	"github.com/go-rotini/jsonschema"
)

type User struct {
	Name  string `json:"name"  jsonschema:"required,minLength=1"`
	Email string `json:"email" jsonschema:"required,format=email"`
	Age   int    `json:"age,omitempty" jsonschema:"minimum=0,maximum=150"`
}

func main() {
	// Compile a schema once; validate many instances against it.
	schemaJSON := []byte(`{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type": "object",
		"properties": {
			"name":  {"type": "string", "minLength": 1},
			"email": {"type": "string", "format": "email"}
		},
		"required": ["name", "email"]
	}`)

	schema, err := jsonschema.Compile(schemaJSON)
	if err != nil {
		log.Fatal(err)
	}

	// Validate an instance.
	instance := []byte(`{"name": "Ada", "email": "ada@example.com"}`)
	result, err := schema.Validate(instance)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("valid:", result.Valid)

	// Validate then decode into a typed value in one call.
	user, err := jsonschema.ValidateTo[User](schema, instance)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%+v\n", user)

	// Generate a schema from a Go type.
	generated, err := jsonschema.GenerateBytes(User{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(generated))
}
```

## Integrating With encoding/json

The standard library does not have a schema layer, so there is no drop-in replacement to mirror. Instead, the two libraries integrate at the boundary: `Schema.ValidateAndUnmarshal` validates `instanceJSON` against the schema and, on success, decodes it via `encoding/json.Unmarshal` in a single call.

```go
var u User
if err := schema.ValidateAndUnmarshal(instance, &u); err != nil {
	// err is either a *jsonschema.ValidationError chain or a json.Unmarshal error.
	log.Fatal(err)
}
```

`Schema.MarshalJSON` returns canonical schema bytes that round-trip through `encoding/json.Unmarshal` + `CompileValue`.

## Schema Generation From Go Types

Two tags drive generation:

- `json` — standard library semantics: property name, `omitempty`, `-`.
- `jsonschema` — schema-specific options.

Tag option vocabulary:

| Option | JSON Schema keyword |
|---|---|
| `required` | adds field to parent `required` |
| `description=...` | `description` |
| `title=...` | `title` |
| `default=...` | `default` |
| `examples=v1\|v2` | `examples` |
| `deprecated`, `readOnly`, `writeOnly` | flag |
| `enum=a\|b\|c` | `enum` |
| `const=v` | `const` |
| `format=name` | `format` |
| `minimum=N`, `maximum=N`, `exclusiveMinimum=N`, `exclusiveMaximum=N`, `multipleOf=N` | numeric |
| `minLength=N`, `maxLength=N`, `pattern=re` | string |
| `minItems=N`, `maxItems=N`, `uniqueItems` | array |
| `minProperties=N`, `maxProperties=N`, `additionalProperties=false` | object |
| `$id=uri`, `$ref=uri` | identity / reference |

Self-referential types are emitted as `$defs` entries with internal `$ref` links.

## Comparison

| Feature | santhosh-tekuri/v5 | xeipuuv | kaptinlin | invopop | **go-rotini/jsonschema** |
|---|---|---|---|---|---|
| Draft 2020-12 | yes | no | yes | n/a | **yes** |
| Draft 2019-09 | yes | no | yes | n/a | **yes** |
| Draft 7 / 6 / 4 | yes | yes | yes | n/a | **yes** |
| Compile/validate split | yes | yes | yes | n/a | **yes** |
| All four output formats | partial | no | yes | n/a | **yes** |
| Generic `ValidateTo[T]` | no | no | no | n/a | **yes** |
| Multi-format instance input | no | no | no | n/a | **yes** |
| Schema generation from Go types | no | no | partial | yes | **yes** |
| Custom keywords / vocabularies | yes | partial | yes | n/a | **yes** |
| Source-pointer error formatting | no | partial | partial | n/a | **yes** |
| DoS protection (depth/size/refs) | partial | partial | yes | n/a | **yes** (all four) |

`n/a` means the package is single-purpose and the row does not apply.

## Documentation

Full API reference is available on [pkg.go.dev](https://pkg.go.dev/github.com/go-rotini/jsonschema).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines on how to contribute to this project.

## Code of Conduct

This project follows a code of conduct to ensure a welcoming community. See [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

## Security

To report a vulnerability, see [SECURITY.md](SECURITY.md).

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.
