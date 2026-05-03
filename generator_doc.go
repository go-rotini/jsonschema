package jsonschema

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
)

// lookupFieldDoc returns the doc comment associated with sf, looked up in
// the generator's doc-comment cache. owner is the struct type the field
// belongs to (so disambiguation is possible across packages).
//
// Returns the empty string when [WithGenerateOmitDescriptions] is set, when
// the generator has no doc reader, or when no comment was found for the
// field.
func (c *genCtx) lookupFieldDoc(owner reflect.Type, sf reflect.StructField) string {
	if c.g.opts.omitDescriptions {
		return ""
	}
	if c.g.opts.docReaderFS == nil {
		return ""
	}
	cache := c.g.loadDocCache()
	if len(cache) == 0 {
		return ""
	}
	if owner != nil && owner.Name() != "" {
		key := owner.Name() + "." + sf.Name
		if v, ok := cache[key]; ok {
			return v
		}
	}
	if v, ok := cache[sf.Name]; ok {
		return v
	}
	return ""
}

// loadDocCache reads the configured fs.FS once, parses each *.go file, and
// returns a map keyed by "<TypeName>.<Field>" plus bare "<Field>"
// fallbacks. Errors are non-fatal: the cache simply ends up empty (or
// partial) and lookups return "".
func (g *Generator) loadDocCache() map[string]string {
	g.docOnce.Do(func() {
		if g.opts.docReaderFS == nil {
			return
		}
		g.docCache = buildDocCache(g.opts.docReaderFS)
	})
	return g.docCache
}

// buildDocCache walks the fs.FS, parses each `.go` file, and harvests
// per-field doc comments. The map is keyed by:
//   - `<TypeName>.<FieldName>` for type-qualified lookups, and
//   - `<FieldName>` as a convenience fallback.
//
// Conflicts between fallbacks are resolved last-write-wins; callers that
// need precise targeting should write a `description=` tag instead. Per-
// file errors are intentionally swallowed — the doc reader is documented
// as best-effort and must not fail generation when a Go source is
// malformed or unreadable.
func buildDocCache(fsys fs.FS) map[string]string {
	out := make(map[string]string)
	fset := token.NewFileSet()
	walkErr := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // best-effort: skip on per-entry error
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return nil //nolint:nilerr // best-effort: skip unreadable files
		}
		f, err := parser.ParseFile(fset, path, data, parser.ParseComments)
		if err != nil {
			return nil //nolint:nilerr // best-effort: skip malformed Go files
		}
		harvestFile(f, out)
		return nil
	})
	_ = walkErr // best-effort; no caller to surface to.
	return out
}

// harvestFile pulls type-spec field doc-comments out of a parsed file and
// writes them into out. Only struct types are considered (interfaces and
// other shapes don't apply to the generator's reflection walk).
func harvestFile(f *ast.File, out map[string]string) {
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok || st.Fields == nil {
				continue
			}
			for _, field := range st.Fields.List {
				doc := fieldDocText(field)
				if doc == "" {
					continue
				}
				for _, name := range field.Names {
					out[ts.Name.Name+"."+name.Name] = doc
					if _, taken := out[name.Name]; !taken {
						out[name.Name] = doc
					}
				}
			}
		}
	}
}

// fieldDocText extracts the doc text associated with a field declaration.
// Prefers a leading doc comment block; falls back to a trailing line
// comment on the same line. Returns the trimmed first paragraph so common
// godoc-style multi-line comments collapse into one description string.
func fieldDocText(field *ast.Field) string {
	if field.Doc != nil {
		return condenseComment(field.Doc.Text())
	}
	if field.Comment != nil {
		return condenseComment(field.Comment.Text())
	}
	return ""
}

// condenseComment turns a multi-line godoc string into a single-line
// description by joining non-blank lines with a space.
func condenseComment(in string) string {
	in = strings.TrimSpace(in)
	if in == "" {
		return ""
	}
	lines := strings.Split(in, "\n")
	var parts []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts = append(parts, line)
	}
	return strings.Join(parts, " ")
}
