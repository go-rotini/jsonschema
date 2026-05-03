module github.com/go-rotini/jsonschema/multifmt

go 1.26.2

require (
	github.com/go-rotini/jsonc v0.0.0-00010101000000-000000000000
	github.com/go-rotini/jsonschema v0.0.0-00010101000000-000000000000
	github.com/go-rotini/toml v0.0.0-00010101000000-000000000000
	github.com/go-rotini/yaml v0.0.0-00010101000000-000000000000
)

replace github.com/go-rotini/jsonschema => ..

replace github.com/go-rotini/jsonc => ../../jsonc

replace github.com/go-rotini/yaml => ../../yaml

replace github.com/go-rotini/toml => ../../toml
