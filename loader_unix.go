//go:build unix

package jsonschema

import (
	"os"
	"syscall"
)

// openReadFlags opens a file for reading without blocking on a named pipe
// that has no writer.
const openReadFlags = os.O_RDONLY | syscall.O_NONBLOCK
