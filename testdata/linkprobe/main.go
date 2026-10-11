// Command linkprobe imports jsonschema without compiling a schema. The
// package tests build it to check what such a program links.
package main

import (
	"fmt"

	"github.com/go-rotini/jsonschema"
)

func main() { fmt.Println(jsonschema.Draft202012) }
