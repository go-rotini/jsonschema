package jsonschema

import (
	"fmt"
	"os"
)

// readFileImpl is the production read hook for FileLoader; tests can
// override the readFile var in loader.go.
func readFileImpl(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	return data, nil
}
