package jsonschema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// benchSmallSchema is a flat object schema of the size a CLI config
// section typically has.
const benchSmallSchema = `{
  "type": "object",
  "properties": {
    "name": {"type": "string", "minLength": 1},
    "port": {"type": "integer", "minimum": 1, "maximum": 65535},
    "debug": {"type": "boolean"},
    "tags": {"type": "array", "items": {"type": "string"}}
  },
  "required": ["name"],
  "additionalProperties": false
}`

const benchSmallInstance = `{"name":"svc","port":8080,"debug":true,"tags":["a","b"]}`

func readBenchFixture(b *testing.B, name string) []byte {
	b.Helper()
	data, err := os.ReadFile(filepath.Join(acceptanceFixtureDir, name))
	if err != nil {
		b.Fatal(err)
	}
	return data
}

// BenchmarkCompile measures compiling small, medium and large schemas.
func BenchmarkCompile(b *testing.B) {
	cases := []struct {
		name string
		src  []byte
	}{
		{"small", []byte(benchSmallSchema)},
		{"medium", readBenchFixture(b, "kitchen-sink.json")},
		{"large", readBenchFixture(b, "openapi-3.1.json")},
		{"xlarge", readBenchFixture(b, "asyncapi-2.6.json")},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Compile(c.src); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkValidateFixtures measures passing and failing validations
// against the acceptance fixtures.
func BenchmarkValidateFixtures(b *testing.B) {
	cases := []struct {
		name, schema, instance string
		opts                   []Option
	}{
		{"small/pass", "", benchSmallInstance, nil},
		{"kitchen/pass", "kitchen-sink.json", "kitchen-sink.pass.json", nil},
		{"kitchen/fail", "kitchen-sink.json", "kitchen-sink.fail.json", nil},
		{"kitchen/formats", "kitchen-sink.json", "kitchen-sink.pass.json", []Option{WithFormatAssertion(true)}},
		{"kitchen/noannot", "kitchen-sink.json", "kitchen-sink.pass.json", []Option{WithCollectAnnotations(false)}},
		{"openapi/pass", "openapi-3.1.json", "openapi-3.1.pass.json", nil},
		{"asyncapi/pass", "asyncapi-2.6.json", "asyncapi-2.6.pass.json", nil},
		{"geojson/pass", "geojson.json", "geojson.pass.json", nil},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			var s *Schema
			var inst []byte
			if c.schema == "" {
				s = MustCompile([]byte(benchSmallSchema))
				inst = []byte(c.instance)
			} else {
				s = MustCompile(readBenchFixture(b, c.schema))
				inst = readBenchFixture(b, filepath.Join("instances", c.instance))
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := s.Validate(inst, c.opts...); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkValidateMetaSchema validates a large schema document against
// the Draft 2020-12 meta-schema: a deep, $dynamicRef-heavy evaluation.
func BenchmarkValidateMetaSchema(b *testing.B) {
	meta, err := CompileURL("https://json-schema.org/draft/2020-12/schema")
	if err != nil {
		b.Fatal(err)
	}
	var doc any
	if err := json.Unmarshal(readBenchFixture(b, "kitchen-sink.json"), &doc); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := meta.ValidateValue(doc); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkOutput renders a failing result in each output format.
func BenchmarkOutput(b *testing.B) {
	s := MustCompile(readBenchFixture(b, "kitchen-sink.json"))
	res, err := s.Validate(readBenchFixture(b, "instances/kitchen-sink.fail.json"))
	if err != nil {
		b.Fatal(err)
	}
	for _, f := range []struct {
		name string
		f    OutputFormat
	}{{"basic", OutputBasic}, {"detailed", OutputDetailed}, {"verbose", OutputVerbose}} {
		b.Run(f.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = res.Output(f.f)
			}
		})
	}
}
