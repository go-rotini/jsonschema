package generate_test

import (
	"fmt"

	"github.com/go-rotini/jsonschema/generate"
)

// ExampleGenerate emits a JSON Schema describing a Go struct.
func ExampleGenerate() {
	type User struct {
		Name string `json:"name" jsonschema:"required,minLength=1"`
	}
	schema, err := generate.Generate((*User)(nil))
	if err != nil {
		fmt.Println("err:", err)
		return
	}
	out, _ := schema.MarshalJSON()
	fmt.Println(string(out))
	// Output:
	// {"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"name":{"type":"string","minLength":1}},"required":["name"]}
}
