package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// errReader / errWriter fail every operation, to exercise the I/O error paths.
// They are shared across the package's tests (generate_test.go uses them too).
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("boom") }

// ── command dispatch (run) ──────────────────────────────────────────────────

func TestRun_noCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(nil, strings.NewReader(""), &out, &errOut); code != 2 {
		t.Fatalf("no command: exit=%d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "usage: jsonschema <command>") {
		t.Errorf("no command: stderr missing usage:\n%s", errOut.String())
	}
}

func TestRun_unknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"bogus"}, strings.NewReader(""), &out, &errOut)
	if code != 2 {
		t.Fatalf("unknown command: exit=%d, want 2", code)
	}
	if !strings.Contains(errOut.String(), `unknown command "bogus"`) {
		t.Errorf("unknown command: stderr missing the rejection:\n%s", errOut.String())
	}
}

func TestRun_help(t *testing.T) {
	for _, arg := range []string{"help", "-h", "--help"} {
		var out, errOut bytes.Buffer
		if code := run([]string{arg}, strings.NewReader(""), &out, &errOut); code != 0 {
			t.Fatalf("%s: exit=%d, want 0", arg, code)
		}
		for _, want := range []string{"generate", "bowtie"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("%s: stdout missing %q in the commands list:\n%s", arg, want, out.String())
			}
		}
	}
}

// TestRun_bowtieDispatch confirms `run` routes the "bowtie" command into the
// connector (a start+stop transcript yields the implementation descriptor). The
// protocol itself is covered in depth in bowtie_test.go.
func TestRun_bowtieDispatch(t *testing.T) {
	in := strings.NewReader(`{"cmd":"start","version":1}` + "\n" + `{"cmd":"stop"}` + "\n")
	var out, errOut bytes.Buffer
	if code := run([]string{"bowtie"}, in, &out, &errOut); code != 0 {
		t.Fatalf("bowtie dispatch: exit=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), implementationName) {
		t.Errorf("bowtie dispatch: start response missing %q:\n%s", implementationName, out.String())
	}
}
