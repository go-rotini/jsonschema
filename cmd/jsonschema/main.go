// Command jsonschema is the multi-command CLI for the go-rotini/jsonschema
// package.
//
// Usage:
//
//	jsonschema <command> [flags] [args]
//
// Commands:
//
//	generate   Go type declarations from a JSON Schema
//	bowtie     Bowtie cross-implementation conformance-harness connector
//
// generate reads a JSON Schema (from the file argument, or from standard input
// when no file is given) and writes Go source to standard output, or to the
// file named by -o:
//
//	go tool jsonschema generate -package models -o models.go schema.json
//
// bowtie speaks the Bowtie stdin/stdout JSON protocol (see bowtie.go).
package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run is the testable core of main: it dispatches the named command and returns
// the process exit code.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "generate":
		return runGenerate(rest, stdin, stdout, stderr)
	case "bowtie":
		return runBowtie(rest, stdin, stdout, stderr)
	case "help", "-h", "--help":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "jsonschema: unknown command %q\n", cmd)
		usage(stderr)
		return 2
	}
}

// usage writes the top-level command summary.
func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: jsonschema <command> [flags] [args]")
	fmt.Fprintln(w, "commands:")
	fmt.Fprintln(w, "  generate   Go type declarations from a JSON Schema")
	fmt.Fprintln(w, "  bowtie     Bowtie conformance-harness connector (stdin/stdout protocol)")
}
