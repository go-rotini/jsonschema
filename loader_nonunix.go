//go:build !unix

package jsonschema

import "os"

// openReadFlags opens a file for reading.
const openReadFlags = os.O_RDONLY
