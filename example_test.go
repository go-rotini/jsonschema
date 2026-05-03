package jsonschema_test

import (
	"fmt"

	"github.com/go-rotini/jsonschema"
)

// ExampleCompile shows the one-shot Compile entry point used to amortize
// schema compilation across many validations.
func ExampleCompile() {
	schema, err := jsonschema.Compile([]byte(`{"type":"string","minLength":3}`))
	if err != nil {
		fmt.Println("compile error:", err)
		return
	}
	res, err := schema.Validate([]byte(`"hi"`))
	if err != nil {
		fmt.Println("validate error:", err)
		return
	}
	fmt.Println(res.Valid)
	// Output:
	// false
}

// ExampleSchema_Validate demonstrates validating an instance against a
// previously compiled schema and inspecting the failure list.
func ExampleSchema_Validate() {
	schema := jsonschema.MustCompile([]byte(`{
		"type":"object",
		"properties":{"name":{"type":"string"}},
		"required":["name"]
	}`))
	res, _ := schema.Validate([]byte(`{}`))
	fmt.Println(res.Valid, len(res.Errors) > 0)
	// Output:
	// false true
}

// ExampleValidate is the one-shot Compile + Validate convenience for
// callers that only validate one instance and don't need to retain the
// compiled [*jsonschema.Schema].
func ExampleValidate() {
	res, err := jsonschema.Validate(
		[]byte(`{"type":"integer","minimum":0}`),
		[]byte(`-5`),
	)
	if err != nil {
		fmt.Println("err:", err)
		return
	}
	fmt.Println(res.Valid)
	// Output:
	// false
}

// ExampleSchema_Validate_integer demonstrates compiling a schema with a
// numeric type assertion and validating an integer instance.
func ExampleSchema_Validate_integer() {
	schema := jsonschema.MustCompile([]byte(`{"type":"integer"}`))
	res, err := schema.Validate([]byte(`42`))
	if err != nil {
		fmt.Println("err:", err)
		return
	}
	fmt.Println(res.Valid)
	// Output:
	// true
}

// ExampleGenerate emits a JSON Schema describing a Go struct.
func ExampleGenerate() {
	type User struct {
		Name string `json:"name" jsonschema:"required,minLength=1"`
	}
	schema, err := jsonschema.Generate((*User)(nil))
	if err != nil {
		fmt.Println("err:", err)
		return
	}
	out, _ := schema.MarshalJSON()
	fmt.Println(string(out))
	// Output:
	// {"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"name":{"type":"string","minLength":1}},"required":["name"]}
}

// ExampleResult_Output renders a validation result in OutputFlag form.
func ExampleResult_Output() {
	schema := jsonschema.MustCompile([]byte(`{"type":"number"}`))
	res, _ := schema.Validate([]byte(`"oops"`))
	fmt.Println(string(res.Output(jsonschema.OutputFlag)))
	// Output:
	// {"valid":false}
}

// ExampleNewCompiler reuses one Compiler across multiple Compile calls so
// that any external schemas referenced via $ref are fetched once and cached.
func ExampleNewCompiler() {
	c := jsonschema.NewCompiler()
	a, _ := c.Compile([]byte(`{"type":"string"}`))
	b, _ := c.Compile([]byte(`{"type":"integer"}`))
	r1, _ := a.Validate([]byte(`"x"`))
	r2, _ := b.Validate([]byte(`5`))
	fmt.Println(r1.Valid, r2.Valid)
	// Output:
	// true true
}
