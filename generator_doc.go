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

// buildDocCache walks the fs.FS, parses each Go file, and harvests
// per-field doc comments keyed by "<TypeName>.<FieldName>" plus a bare
// "<FieldName>" fallback (last-write-wins on collision). Per-file errors
// are swallowed: the doc reader is best-effort.
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

// harvestFile collects struct-field doc comments from f into out.
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

// fieldDocText returns the leading doc comment for a field, or — failing
// that — its trailing line comment, condensed to a single line.
func fieldDocText(field *ast.Field) string {
	if field.Doc != nil {
		return condenseComment(field.Doc.Text())
	}
	if field.Comment != nil {
		return condenseComment(field.Comment.Text())
	}
	return ""
}

// condenseComment joins a multi-line godoc string's non-blank lines with
// single spaces.
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
