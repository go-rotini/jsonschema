package jsonschema

import (
	"fmt"
	"os"
)

// readFileImpl is the production implementation of the FileLoader's read
// hook. It exists in its own file so the test code can override it via the
// readFile var in loader.go.
func readFileImpl(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	return data, nil
}
