// Command gentypes generates Go type declarations from a JSON Schema.
//
// Usage:
//
//	gentypes [flags] [schema-file]
//
// The schema is read from the file argument, or from standard input when no
// file is given. Generated Go source is written to standard output, or to
// the file named by -o.
//
// As a go tool:
//
//	go tool gentypes -package models -o models.go schema.json
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/go-rotini/jsonschema"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run is the testable core of main: it parses args, generates types, and
// returns the process exit code.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("gentypes", flag.ContinueOnError)
	flags.SetOutput(stderr)
	pkg := flags.String("package", "models", "package name for the generated file")
	root := flags.String("root", "", `name of the root type (default: schema "title", else "Root")`)
	outPath := flags.String("o", "", "write output to this file instead of stdout")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: gentypes [flags] [schema-file]")
		fmt.Fprintln(stderr, "reads a JSON Schema (from schema-file or stdin) and writes Go types")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return 2
	}

	schemaJSON, err := readSchema(flags.Arg(0), stdin)
	if err != nil {
		fmt.Fprintln(stderr, "gentypes:", err)
		return 1
	}

	opts := []jsonschema.GoOption{jsonschema.WithGoPackage(*pkg)}
	if *root != "" {
		opts = append(opts, jsonschema.WithGoRootType(*root))
	}
	src, err := jsonschema.GenerateGo(schemaJSON, opts...)
	if err != nil {
		fmt.Fprintln(stderr, "gentypes:", err)
		return 1
	}

	if err := writeOutput(*outPath, src, stdout); err != nil {
		fmt.Fprintln(stderr, "gentypes:", err)
		return 1
	}
	return 0
}

// readSchema reads schema bytes from path, or from stdin when path is empty.
func readSchema(path string, stdin io.Reader) ([]byte, error) {
	if path == "" {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read schema file: %w", err)
	}
	return data, nil
}

// writeOutput writes src to path, or to stdout when path is empty.
func writeOutput(path string, src []byte, stdout io.Writer) error {
	if path == "" {
		if _, err := stdout.Write(src); err != nil {
			return fmt.Errorf("write stdout: %w", err)
		}
		return nil
	}
	if err := os.WriteFile(path, src, 0o600); err != nil {
		return fmt.Errorf("write output file: %w", err)
	}
	return nil
}
