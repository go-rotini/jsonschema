//go:build unix

package jsonschema

import (
	"errors"
	"syscall"
	"testing"
	"time"
)

// TestFileLoaderRefusesFIFO checks that a named pipe under Root is refused
// rather than blocking the load until a writer appears.
func TestFileLoaderRefusesFIFO(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(dir+"/s.json", 0o600); err != nil {
		t.Skip("mkfifo:", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := (FileLoader{Root: dir}).Load("file:///s.json")
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, errNotRegularFile) {
			t.Errorf("err = %v; want errNotRegularFile", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Load blocked on a FIFO")
	}
}
