package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"

	"github.com/go-rotini/jsonschema"
	"github.com/go-rotini/jsonschema/gogen"
	"github.com/go-rotini/jsonschema/httploader"
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
	var allowNetwork bool
	flags.BoolVar(&allowNetwork, "allow-network", false, "fetch remote $refs over HTTPS")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: jsonschema generate [flags] [schema-file]")
		fmt.Fprintln(stderr, "reads a JSON Schema (from schema-file or stdin) and writes Go types")
		fmt.Fprintln(stderr, `  -p, --package string   package name for the generated file (default "models")`)
		fmt.Fprintln(stderr, `  -r, --root string      name of the root type (default: schema "title", else "Root")`)
		fmt.Fprintln(stderr, `  -o, --output string    write output to this file instead of stdout`)
		fmt.Fprintln(stderr, `      --allow-network    fetch remote $refs over HTTPS (default: off; only the embedded meta-schemas resolve)`)
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

	opts := []gogen.Option{gogen.WithPackage(pkg)}
	var loaders jsonschema.ChainLoader
	if path := flags.Arg(0); path != "" {
		// Relative $refs resolve to files in the schema file's directory,
		// which FileLoader serves as the root of file:// URIs.
		opts = append(opts, gogen.WithBaseURI((&url.URL{Scheme: "file", Path: "/" + filepath.Base(path)}).String()))
		loaders = append(loaders, jsonschema.FileLoader{Root: filepath.Dir(path)})
	}
	if allowNetwork {
		loaders = append(loaders, httploader.Default())
	}
	if len(loaders) > 0 {
		opts = append(opts, gogen.WithLoader(append(loaders, jsonschema.DefaultLoader())))
	}
	if root != "" {
		opts = append(opts, gogen.WithRootType(root))
	}
	src, err := gogen.Generate(schemaJSON, opts...)
	if err != nil {
		fmt.Fprintln(stderr, "jsonschema:", err)
		if le := (*jsonschema.LoaderError)(nil); !allowNetwork && errors.As(err, &le) &&
			errors.Is(err, jsonschema.ErrLoaderRejected) && isHTTPURI(le.URI) {
			fmt.Fprintln(stderr, "jsonschema: pass --allow-network to fetch remote $refs")
		}
		return 1
	}

	if err := writeOutput(outPath, src, stdout); err != nil {
		fmt.Fprintln(stderr, "jsonschema:", err)
		return 1
	}
	return 0
}

// isHTTPURI reports whether uri is an http or https URI.
func isHTTPURI(uri string) bool {
	u, err := url.Parse(uri)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https")
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
	if err := os.WriteFile(path, src, 0o644); err != nil { //nolint:gosec // generated Go source is shared like any other source file
		return fmt.Errorf("write output file: %w", err)
	}
	return nil
}
