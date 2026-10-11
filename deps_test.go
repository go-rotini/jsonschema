package jsonschema

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestRootPackageDependencies keeps network, archive, template and
// Go-toolchain packages out of the root package, so a program that only
// validates doesn't link them. Those live in the httploader, generate and
// gogen subpackages. Each entry also forbids the packages below it.
func TestRootPackageDependencies(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list")
	}
	forbidden := []string{
		"net/http", "crypto/tls", "crypto/x509",
		"go", "archive", "compress/gzip", "text/template",
	}
	for _, goos := range []string{"linux", "darwin", "windows"} {
		cmd := exec.Command("go", "list", "-deps", ".")
		cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH=amd64", "CGO_ENABLED=0")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list -deps (GOOS=%s): %v", goos, err)
		}
		for dep := range strings.FieldsSeq(string(out)) {
			for _, pkg := range forbidden {
				if dep == pkg || strings.HasPrefix(dep, pkg+"/") {
					t.Errorf("GOOS=%s: the root package depends on %s", goos, dep)
				}
			}
		}
	}
}
