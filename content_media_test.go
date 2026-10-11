package jsonschema

import (
	"mime"
	"strings"
	"testing"
)

// isJSONMediaTypeMime is isJSONMediaType as written with mime.ParseMediaType,
// the reference the parser-free version must match.
func isJSONMediaTypeMime(mt string) bool {
	main, _, err := mime.ParseMediaType(mt)
	if err != nil {
		main = strings.TrimSpace(strings.ToLower(strings.SplitN(mt, ";", 2)[0]))
	}
	main = strings.ToLower(main)
	if main == "application/json" || main == "text/json" {
		return true
	}
	return strings.HasSuffix(main, "+json")
}

func FuzzIsJSONMediaType(f *testing.F) {
	for _, s := range []string{
		"", "application/json", "Application/JSON", " application/json ", "application/json; charset=utf-8",
		"text/json", "application/ld+json", "application/vnd.api+json;q=1", "application/json;", "application/json; a",
		"application/json; a=\"b", "application/json; a=1; a=2", "application/json; a*=utf-8''%41", "application/ json",
		"application/json\t;x=y", "json", "+json", "application/jsonx", "application/xml", "text/plain; x=+json",
		"APPLICATION/X+JSON ; charset=\"utf-8\"", "Kapplication/json", "application/json ", "a/b+JSON",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, mt string) {
		if got, want := isJSONMediaType(mt), isJSONMediaTypeMime(mt); got != want {
			t.Errorf("isJSONMediaType(%q) = %v, mime.ParseMediaType reading gives %v", mt, got, want)
		}
	})
}
