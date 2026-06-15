package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/go-rotini/jsonschema"
)

// runGenerate parses the generate flags, generates Go types from a JSON Schema,
// and returns the exit code. Each flag has a short and a long form bound to the
// same variable: -p/--package, -r/--root, -o/--output.
func runGenerate(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("jsonschema generate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var pkg, root, outPath string
	flags.StringVar(&pkg, "package", "models", "package name for the generated file")
	flags.StringVar(&pkg, "p", "models", "")
	flags.StringVar(&root, "root", "", `name of the root type (default: schema "title", else "Root")`)
	flags.StringVar(&root, "r", "", "")
	flags.StringVar(&outPath, "output", "", "write output to this file instead of stdout")
	flags.StringVar(&outPath, "o", "", "")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: jsonschema generate [flags] [schema-file]")
		fmt.Fprintln(stderr, "reads a JSON Schema (from schema-file or stdin) and writes Go types")
		fmt.Fprintln(stderr, `  -p, --package string   package name for the generated file (default "models")`)
		fmt.Fprintln(stderr, `  -r, --root string      name of the root type (default: schema "title", else "Root")`)
		fmt.Fprintln(stderr, `  -o, --output string    write output to this file instead of stdout`)
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	schemaJSON, err := readSchema(flags.Arg(0), stdin)
	if err != nil {
		fmt.Fprintln(stderr, "jsonschema:", err)
		return 1
	}

	opts := []jsonschema.GoOption{jsonschema.WithGoPackage(pkg)}
	if root != "" {
		opts = append(opts, jsonschema.WithGoRootType(root))
	}
	src, err := jsonschema.GenerateGo(schemaJSON, opts...)
	if err != nil {
		fmt.Fprintln(stderr, "jsonschema:", err)
		return 1
	}

	if err := writeOutput(outPath, src, stdout); err != nil {
		fmt.Fprintln(stderr, "jsonschema:", err)
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
