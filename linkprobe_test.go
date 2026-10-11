package jsonschema

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestImportOnlyLinksLittle builds testdata/linkprobe, a program that
// imports the package without compiling a schema, and checks that the
// keyword evaluators, the compiler and their dependencies are not linked:
// nothing at package init may keep them reachable.
func TestImportOnlyLinksLittle(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go build")
	}
	bin := filepath.Join(t.TempDir(), "linkprobe")
	if out, err := exec.Command("go", "build", "-o", bin, "./testdata/linkprobe").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	out, err := exec.Command("go", "tool", "nm", bin).Output()
	if err != nil {
		t.Fatalf("go tool nm: %v", err)
	}
	forbidden := []string{
		"github.com/go-rotini/jsonschema.registerBuiltinKeywords",
		"github.com/go-rotini/jsonschema.(*Compiler).Compile",
		"github.com/go-rotini/jsonschema.embeddedMetaMapLoader",
		"math/big.",
		"regexp.Compile",
	}
	for line := range strings.Lines(string(out)) {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		sym := fields[len(fields)-1]
		for _, f := range forbidden {
			if strings.HasPrefix(sym, f) {
				t.Errorf("linked: %s", sym)
			}
		}
	}
}
