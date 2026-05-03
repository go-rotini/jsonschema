package jsonschema

import (
	"strings"
	"testing"
	"testing/fstest"
)

// docFixture is the type the doc-reader test describes. The actual struct
// declaration here doesn't carry doc comments — the [WithDocReader] feature
// reads from an in-memory fs.FS containing a parallel Go source file.
type docFixture struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func TestWithDocReaderHonorsComments(t *testing.T) {
	src := `package fixture

// docFixture carries a fixture used by the generator doc-reader test.
type docFixture struct {
	// Name is the user's full display name.
	Name string ` + "`json:\"name\"`" + `
	// Age is the user's age in years.
	Age int ` + "`json:\"age\"`" + `
}
`
	fsys := fstest.MapFS{
		"fixture.go": &fstest.MapFile{Data: []byte(src)},
	}
	data, err := GenerateBytes(docFixture{}, WithDocReader(fsys))
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"description":"Name is the user's full display name."`) {
		t.Errorf("missing Name description: %s", data)
	}
	if !strings.Contains(string(data), `"description":"Age is the user's age in years."`) {
		t.Errorf("missing Age description: %s", data)
	}
}

func TestWithDocReaderTagWins(t *testing.T) {
	type fixture struct {
		Name string `json:"name" jsonschema:"description=tag-wins"`
	}
	src := `package fixture

type fixture struct {
	// Name is described in the source file.
	Name string ` + "`json:\"name\"`" + `
}
`
	fsys := fstest.MapFS{
		"fixture.go": &fstest.MapFile{Data: []byte(src)},
	}
	data, err := GenerateBytes(fixture{}, WithDocReader(fsys))
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if !strings.Contains(string(data), `"description":"tag-wins"`) {
		t.Errorf("expected tag to win, got %s", data)
	}
	if strings.Contains(string(data), `Name is described in the source file`) {
		t.Errorf("doc comment should have been suppressed by tag: %s", data)
	}
}

func TestWithDocReaderUnreadableFilesAreIgnored(t *testing.T) {
	// A non-Go file in the fs.FS should be skipped silently; a malformed
	// .go file should also not stop generation.
	fsys := fstest.MapFS{
		"README.md": &fstest.MapFile{Data: []byte("# hi")},
		"broken.go": &fstest.MapFile{Data: []byte("not valid go")},
	}
	type Holder struct {
		Name string `json:"name"`
	}
	if _, err := GenerateBytes(Holder{}, WithDocReader(fsys)); err != nil {
		t.Fatalf("GenerateBytes should not fail on bad doc-reader input: %v", err)
	}
}

func TestWithDocReaderOmitDescriptionsOverrides(t *testing.T) {
	src := `package fixture

type fixture struct {
	// Name has a doc comment that should be suppressed.
	Name string ` + "`json:\"name\"`" + `
}
`
	fsys := fstest.MapFS{
		"fixture.go": &fstest.MapFile{Data: []byte(src)},
	}
	type fixture struct {
		Name string `json:"name"`
	}
	data, err := GenerateBytes(fixture{}, WithDocReader(fsys), WithGenerateOmitDescriptions(true))
	if err != nil {
		t.Fatalf("GenerateBytes: %v", err)
	}
	if strings.Contains(string(data), `"description"`) {
		t.Errorf("expected no descriptions; got %s", data)
	}
}
