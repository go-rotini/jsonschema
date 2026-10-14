package jsonschema

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

var (
	errNotRegularFile = errors.New("not a regular file")
	errFileTooLarge   = errors.New("file too large")
)

// defaultMaxFileSize is the FileLoader size limit when MaxSize is zero.
const defaultMaxFileSize = 64 << 20

// readFileImpl is the production read hook for FileLoader; tests can
// override the readFile var in loader.go. It opens rel through an [os.Root]
// for dir, so a symlink swapped in after the containment checks still
// cannot lead outside dir, and reads at most maxSize bytes of a regular
// file (no limit when maxSize is negative).
func readFileImpl(dir, rel string, maxSize int64) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open root: %w", err)
	}
	defer func() { _ = root.Close() }()
	// Refuse a named pipe or device before opening it, since opening one
	// for reading can block. The open itself does not block either (see
	// openReadFlags), and the check after it catches a file swapped in
	// between.
	if info, err := root.Stat(rel); err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	} else if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("read file: %w", errNotRegularFile)
	}
	f, err := root.OpenFile(rel, openReadFlags, 0)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("read file: %w", errNotRegularFile)
	}
	if maxSize == 0 {
		maxSize = defaultMaxFileSize
	}
	var r io.Reader = f
	if maxSize > 0 && maxSize < math.MaxInt64 {
		r = io.LimitReader(f, maxSize+1)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if maxSize > 0 && int64(len(data)) > maxSize {
		return nil, fmt.Errorf("%w: over %d bytes", errFileTooLarge, maxSize)
	}
	return data, nil
}
