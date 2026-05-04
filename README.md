# go-rotini/jsonschema

A Go [JSON Schema](https://json-schema.org/) package that compiles, validates, and generates schemas, with multi-format input (JSON, JSONC, YAML, TOML).

This package is used as the default JSON Schema support package for [rotini](https://github.com/go-rotini/rotini).

## Features

- Full [Draft 2020-12](https://json-schema.org/draft/2020-12/schema) support, plus read-only support for Draft 2019-09, Draft 7, Draft 6, and Draft 4
- Compile/validate split (`Compile` → `*Schema` → `Validate`) so compilation amortizes across many validations
- All four output formats from Draft 2020-12 §12: Flag, Basic, Detailed, Verbose
- Generic `ValidateTo[T]` typed-decode in one call
- Schema generation from Go types via reflection (`Generate`, `GenerateBytes`, `FromType`)
- Multi-format instance and schema input (JSONC, YAML, TOML) via `LoadJSONC` / `LoadYAML` / `LoadTOML` and their `Validate*` counterparts
- `$ref`, `$dynamicRef`, `$recursiveRef`, plain-name anchors, and pluggable `Loader` (HTTPS-only by default; opt-in HTTP / file)
- Built-in format validators: `date-time`, `date`, `time`, `duration`, `email`, `idn-email`, `hostname`, `idn-hostname`, `ipv4`, `ipv6`, `uri`, `uri-reference`, `iri`, `iri-reference`, `uri-template`, `json-pointer`, `relative-json-pointer`, `uuid`, `regex`
- Content vocabulary: `contentEncoding`, `contentMediaType`, `contentSchema` (annotation-only by default)
- Two strict modes: `WithMetaSchemaValidation` (compile-time meta-schema check) and `WithFormatAssertion` (runtime format assertion)
- OpenAPI 3.1 dialect support (`VocabOAS`, `OASDialectURL`)
- Bowtie connector for cross-implementation conformance testing
- `RenderError` for human-readable validation errors with a source pointer into the instance
- DoS protection: max ref depth, max recursion depth, max document size, ref-loop detection

## Installation

```bash
go get github.com/go-rotini/jsonschema
```

Requires Go 1.26 or later.

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

```go
type Tree struct {
	Label    string  `json:"label"`
	Children []*Tree `json:"children,omitempty"`
}

schema, _ := jsonschema.GenerateBytes(Tree{})
fmt.Println(string(schema))
// {"$schema":"https://json-schema.org/draft/2020-12/schema",
//  "$ref":"#/$defs/Tree",
//  "$defs":{"Tree":{"type":"object","properties":{
//    "label":{"type":"string"},
//    "children":{"type":"array","items":{"$ref":"#/$defs/Tree"}}}}}}
```

## Multi-Format Input

Schemas and instances can be authored as JSONC, YAML, or TOML. The adapters delegate to the corresponding rotini packages and preserve numeric literals via `json.Number` so `multipleOf` / `minimum` / `maximum` evaluate against the wire form.

```go
schemaYAML := []byte(`
type: object
properties:
  port:
    type: integer
    minimum: 1
    maximum: 65535
required: [port]
`)

schema, err := jsonschema.LoadYAML(schemaYAML)
if err != nil {
	log.Fatal(err)
}

instanceTOML := []byte(`port = 8080`)
result, err := jsonschema.ValidateTOML(schema, instanceTOML)
if err != nil {
	log.Fatal(err)
}
fmt.Println("valid:", result.Valid)
```

Equivalent entry points exist for JSONC: `LoadJSONC` and `ValidateJSONC`.

## Output Formats

`Result.Output` renders a validation result in any of the four formats from Draft 2020-12 §12 — Flag, Basic, Detailed, Verbose.

```go
schema := jsonschema.MustCompile([]byte(`{
	"type": "object",
	"properties": {"name": {"type": "string", "minLength": 3}},
	"required": ["name"]
}`))

result, _ := schema.Validate([]byte(`{"name": "x"}`))

fmt.Println(string(result.Output(jsonschema.OutputFlag)))
// {"valid":false}

fmt.Println(string(result.Output(jsonschema.OutputBasic)))
// {"valid":false,"keywordLocation":"","instanceLocation":"","errors":[
//   {"valid":false,"keywordLocation":"","instanceLocation":"","error":"validation failed"},
//   {"valid":false,"keywordLocation":"/properties/name/minLength",
//    "instanceLocation":"/name","error":"string length 1 is below minLength 3"}
// ]}
```

`OutputDetailed` emits a nested tree pruned to failing branches; `OutputVerbose` keeps the full tree including passing groups. The wire shape validates against the spec's output meta-schema (also exposed via `OutputMetaSchema`).

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
