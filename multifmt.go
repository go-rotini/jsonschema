package jsonschema

// This file provides multi-format input adapters that load JSON Schema
// schemas and instances from JSONC, YAML, or TOML in addition to plain
// JSON. The adapters delegate to the sister go-rotini format packages
// ([github.com/go-rotini/jsonc], [github.com/go-rotini/yaml],
// [github.com/go-rotini/toml]).
//
// # Number precision
//
// Each adapter preserves the original numeric source text wherever the
// sister parser gives it to us, so number-precision keywords like
// multipleOf, minimum, maximum, and const evaluate against the wire form
// rather than against an IEEE-754 round-trip.
//
//   - LoadJSONC / ValidateJSONC: numbers come from jsonc.UnmarshalWithOptions
//     under [github.com/go-rotini/jsonc.WithUseNumber], producing json.Number.
//   - LoadYAML  / ValidateYAML:  the package walks the YAML AST returned by
//     yaml.Parse, converting plain scalars that lex as numbers into
//     json.Number with the original text. Quoted scalars stay strings.
//     Aliases are resolved during the walk.
//   - LoadTOML  / ValidateTOML:  the package walks the TOML AST returned by
//     toml.Parse. Integer literals (decimal, hex, octal, binary, with
//     underscores) are normalized to base-10 json.Number; float literals
//     keep their text minus underscores. Datetime values are converted to
//     RFC3339 strings since the JSON Schema validator handles them as
//     strings under format: date-time.
//
// # Concurrency
//
// All exported multi-format functions are stateless and safe for
// concurrent use. The returned [*Schema] inherits the package's
// concurrency guarantees.
//
// # Limitations
//
//   - YAML's core schema does not natively distinguish arbitrary-precision
//     integers from float64 the way JSON's grammar implies; the AST walk
//     keeps the original text so json.Number-aware keywords work, but
//     if a downstream consumer of [Schema.ValidateValue] strips the
//     json.Number values back to float64 before re-validation, large
//     integers may lose precision.
//   - TOML's offset-date-time, local-date-time, local-date, and local-time
//     values are flattened to strings (RFC3339-style for full datetimes,
//     date-only/time-only forms for the local-* variants). Validate them
//     against {"type": "string", "format": "date-time"} (or "date" / "time").

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/go-rotini/jsonc"
	"github.com/go-rotini/toml"
	"github.com/go-rotini/yaml"
)

// Sentinel errors returned by the multi-format adapters. All errors
// emitted by these adapters wrap one of these so callers can match via
// [errors.Is].
var (
	// ErrInvalidYAML indicates a structural problem in YAML input that the
	// adapter could not convert to a JSON-compatible value (e.g. unresolved
	// aliases, cyclic merges, or multi-document streams).
	ErrInvalidYAML = errors.New("jsonschema: invalid yaml")
	// ErrInvalidTOML indicates a structural problem in TOML input the
	// adapter could not represent as a JSON-compatible value (e.g.
	// integer literal out of range, malformed AST shape).
	ErrInvalidTOML = errors.New("jsonschema: invalid toml")
)

// LoadJSON parses a JSON schema document and compiles it. It is a verbatim
// alias for [Compile] provided for naming symmetry with [LoadJSONC],
// [LoadYAML], and [LoadTOML] when callers dispatch on a runtime format
// label.
func LoadJSON(schemaJSON []byte, opts ...CompileOption) (*Schema, error) {
	return Compile(schemaJSON, opts...)
}

// LoadJSONURL fetches a JSON schema document from uri using the configured
// loader and compiles it. Alias for [CompileURL].
func LoadJSONURL(uri string, opts ...CompileOption) (*Schema, error) {
	return CompileURL(uri, opts...)
}

// LoadJSONValue compiles an already-decoded Go value as a JSON schema.
// Alias for [CompileValue].
func LoadJSONValue(v any, opts ...CompileOption) (*Schema, error) {
	return CompileValue(v, opts...)
}

// MustLoadJSON is the panic-on-error variant of [LoadJSON].
func MustLoadJSON(schemaJSON []byte, opts ...CompileOption) *Schema {
	return MustCompile(schemaJSON, opts...)
}

// MustLoadJSONURL is the panic-on-error variant of [LoadJSONURL].
func MustLoadJSONURL(uri string, opts ...CompileOption) *Schema {
	return MustCompileURL(uri, opts...)
}

// MustLoadJSONValue is the panic-on-error variant of [LoadJSONValue].
func MustLoadJSONValue(v any, opts ...CompileOption) *Schema {
	return MustCompileValue(v, opts...)
}

// LoadJSONC parses a JSONC schema document via [github.com/go-rotini/jsonc]
// and compiles it as a JSON Schema. Comments and trailing commas in the
// source are tolerated; numeric literals retain their original text via
// [json.Number] so multipleOf / minimum / maximum evaluate correctly.
func LoadJSONC(schemaJSONC []byte, opts ...CompileOption) (*Schema, error) {
	v, err := decodeJSONC(schemaJSONC)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: load jsonc: %w", err)
	}
	s, err := CompileValue(v, opts...)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: compile jsonc schema: %w", err)
	}
	return s, nil
}

// LoadYAML parses a YAML schema document and compiles it as a JSON Schema.
// The first document in the stream is used; multi-document streams are
// rejected with [ErrInvalidYAML].
func LoadYAML(schemaYAML []byte, opts ...CompileOption) (*Schema, error) {
	v, err := decodeYAML(schemaYAML)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: load yaml: %w", err)
	}
	s, err := CompileValue(v, opts...)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: compile yaml schema: %w", err)
	}
	return s, nil
}

// LoadTOML parses a TOML schema document and compiles it as a JSON Schema.
func LoadTOML(schemaTOML []byte, opts ...CompileOption) (*Schema, error) {
	v, err := decodeTOML(schemaTOML)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: load toml: %w", err)
	}
	s, err := CompileValue(v, opts...)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: compile toml schema: %w", err)
	}
	return s, nil
}

// LoadJSONCURL fetches a JSONC schema document from uri using the
// configured loader and compiles it. Mirrors [CompileURL] for the JSONC
// adapter.
func LoadJSONCURL(uri string, opts ...CompileOption) (*Schema, error) {
	data, err := loadURLBytes(uri, opts)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: load jsonc url: %w", err)
	}
	return LoadJSONC(data, opts...)
}

// LoadYAMLURL fetches a YAML schema document from uri using the configured
// loader and compiles it. Mirrors [CompileURL] for the YAML adapter.
func LoadYAMLURL(uri string, opts ...CompileOption) (*Schema, error) {
	data, err := loadURLBytes(uri, opts)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: load yaml url: %w", err)
	}
	return LoadYAML(data, opts...)
}

// LoadTOMLURL fetches a TOML schema document from uri using the configured
// loader and compiles it. Mirrors [CompileURL] for the TOML adapter.
func LoadTOMLURL(uri string, opts ...CompileOption) (*Schema, error) {
	data, err := loadURLBytes(uri, opts)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: load toml url: %w", err)
	}
	return LoadTOML(data, opts...)
}

// LoadJSONCValue compiles an already-decoded Go value as if it had been
// produced by the JSONC adapter. Provided for symmetry with [CompileValue]
// when the caller has run their own JSONC decoder.
func LoadJSONCValue(v any, opts ...CompileOption) (*Schema, error) {
	s, err := CompileValue(v, opts...)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: compile jsonc value: %w", err)
	}
	return s, nil
}

// LoadYAMLValue compiles an already-decoded Go value as if it had been
// produced by the YAML adapter. Provided for symmetry with [CompileValue]
// when the caller has run their own YAML decoder.
func LoadYAMLValue(v any, opts ...CompileOption) (*Schema, error) {
	s, err := CompileValue(v, opts...)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: compile yaml value: %w", err)
	}
	return s, nil
}

// LoadTOMLValue compiles an already-decoded Go value as if it had been
// produced by the TOML adapter. Provided for symmetry with [CompileValue]
// when the caller has run their own TOML decoder.
func LoadTOMLValue(v any, opts ...CompileOption) (*Schema, error) {
	s, err := CompileValue(v, opts...)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: compile toml value: %w", err)
	}
	return s, nil
}

// MustLoadJSONC is the panic-on-error variant of [LoadJSONC].
func MustLoadJSONC(schemaJSONC []byte, opts ...CompileOption) *Schema {
	s, err := LoadJSONC(schemaJSONC, opts...)
	if err != nil {
		panic(err)
	}
	return s
}

// MustLoadYAML is the panic-on-error variant of [LoadYAML].
func MustLoadYAML(schemaYAML []byte, opts ...CompileOption) *Schema {
	s, err := LoadYAML(schemaYAML, opts...)
	if err != nil {
		panic(err)
	}
	return s
}

// MustLoadTOML is the panic-on-error variant of [LoadTOML].
func MustLoadTOML(schemaTOML []byte, opts ...CompileOption) *Schema {
	s, err := LoadTOML(schemaTOML, opts...)
	if err != nil {
		panic(err)
	}
	return s
}

// MustLoadJSONCURL is the panic-on-error variant of [LoadJSONCURL].
func MustLoadJSONCURL(uri string, opts ...CompileOption) *Schema {
	s, err := LoadJSONCURL(uri, opts...)
	if err != nil {
		panic(err)
	}
	return s
}

// MustLoadYAMLURL is the panic-on-error variant of [LoadYAMLURL].
func MustLoadYAMLURL(uri string, opts ...CompileOption) *Schema {
	s, err := LoadYAMLURL(uri, opts...)
	if err != nil {
		panic(err)
	}
	return s
}

// MustLoadTOMLURL is the panic-on-error variant of [LoadTOMLURL].
func MustLoadTOMLURL(uri string, opts ...CompileOption) *Schema {
	s, err := LoadTOMLURL(uri, opts...)
	if err != nil {
		panic(err)
	}
	return s
}

// MustLoadJSONCValue is the panic-on-error variant of [LoadJSONCValue].
func MustLoadJSONCValue(v any, opts ...CompileOption) *Schema {
	s, err := LoadJSONCValue(v, opts...)
	if err != nil {
		panic(err)
	}
	return s
}

// MustLoadYAMLValue is the panic-on-error variant of [LoadYAMLValue].
func MustLoadYAMLValue(v any, opts ...CompileOption) *Schema {
	s, err := LoadYAMLValue(v, opts...)
	if err != nil {
		panic(err)
	}
	return s
}

// MustLoadTOMLValue is the panic-on-error variant of [LoadTOMLValue].
func MustLoadTOMLValue(v any, opts ...CompileOption) *Schema {
	s, err := LoadTOMLValue(v, opts...)
	if err != nil {
		panic(err)
	}
	return s
}

// loadURLBytes fetches uri's raw bytes using the loader configured by opts
// (or the package default). The bytes are returned to the caller for
// format-specific decoding.
func loadURLBytes(uri string, opts []CompileOption) ([]byte, error) {
	co := defaultCompileOptions()
	for _, o := range opts {
		o(co)
	}
	loader := co.loader
	if loader == nil {
		loader = DefaultLoader()
	}
	data, err := loader.Load(uri)
	if err != nil {
		return nil, &LoaderError{URI: uri, Cause: err}
	}
	return data, nil
}

// ValidateJSONC decodes data as JSONC and validates the resulting Go value
// against s. Number-precision-sensitive keywords see the original numeric
// text via [json.Number].
func ValidateJSONC(s *Schema, data []byte, opts ...Option) (*Result, error) {
	if s == nil {
		return nil, ErrSchemaNotCompiled
	}
	v, err := decodeJSONC(data)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: validate jsonc: %w", err)
	}
	res, err := s.ValidateValue(v, opts...)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: validate jsonc: %w", err)
	}
	return res, nil
}

// ValidateYAML decodes data as YAML and validates the resulting Go value
// against s.
func ValidateYAML(s *Schema, data []byte, opts ...Option) (*Result, error) {
	if s == nil {
		return nil, ErrSchemaNotCompiled
	}
	v, err := decodeYAML(data)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: validate yaml: %w", err)
	}
	res, err := s.ValidateValue(v, opts...)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: validate yaml: %w", err)
	}
	return res, nil
}

// ValidateTOML decodes data as TOML and validates the resulting Go value
// against s.
func ValidateTOML(s *Schema, data []byte, opts ...Option) (*Result, error) {
	if s == nil {
		return nil, ErrSchemaNotCompiled
	}
	v, err := decodeTOML(data)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: validate toml: %w", err)
	}
	res, err := s.ValidateValue(v, opts...)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: validate toml: %w", err)
	}
	return res, nil
}

// decodeJSONC delegates to the jsonc package with WithUseNumber so numeric
// literals materialize as [json.Number].
func decodeJSONC(data []byte) (any, error) {
	var v any
	if err := jsonc.UnmarshalWithOptions(data, &v, jsonc.WithUseNumber()); err != nil {
		return nil, fmt.Errorf("decode jsonc: %w", err)
	}
	return v, nil
}

// decodeYAML walks the YAML AST and produces a Go value tree where numeric
// plain scalars are converted to [json.Number] and aliases are resolved.
func decodeYAML(data []byte) (any, error) {
	file, err := yaml.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	if len(file.Docs) == 0 {
		return nil, nil //nolint:nilnil // empty stream legitimately decodes to nil
	}
	if len(file.Docs) > 1 {
		return nil, fmt.Errorf("%w: expected a single yaml document, got %d", ErrInvalidYAML, len(file.Docs))
	}
	doc := file.Docs[0]
	if doc == nil {
		return nil, nil //nolint:nilnil // null document
	}
	// DocumentNode wraps a single child; older yaml inputs occasionally
	// arrive already unwrapped, so handle both shapes.
	root := doc
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Children) == 0 {
			return nil, nil //nolint:nilnil // empty doc maps to JSON null
		}
		root = doc.Children[0]
	}
	anchors := map[string]*yaml.Node{}
	collectAnchors(root, anchors)
	return convertYAMLNode(root, anchors, map[*yaml.Node]bool{})
}

func collectAnchors(n *yaml.Node, out map[string]*yaml.Node) {
	if n == nil {
		return
	}
	if n.Anchor != "" {
		out[n.Anchor] = n
	}
	for _, c := range n.Children {
		collectAnchors(c, out)
	}
}

func convertYAMLNode(n *yaml.Node, anchors map[string]*yaml.Node, seen map[*yaml.Node]bool) (any, error) {
	if n == nil {
		return nil, nil //nolint:nilnil // YAML missing node is JSON null
	}
	switch n.Kind {
	case yaml.AliasNode:
		target, ok := anchors[n.Alias]
		if !ok {
			return nil, fmt.Errorf("%w: unresolved alias %q", ErrInvalidYAML, n.Alias)
		}
		if seen[target] {
			return nil, fmt.Errorf("%w: cyclic alias %q", ErrInvalidYAML, n.Alias)
		}
		seen[target] = true
		v, err := convertYAMLNode(target, anchors, seen)
		delete(seen, target)
		return v, err
	case yaml.MappingNode:
		out := make(map[string]any, len(n.Children)/2)
		for i := 0; i+1 < len(n.Children); i += 2 {
			keyNode := n.Children[i]
			valNode := n.Children[i+1]
			// Merge key (<< : *anchor) — flatten mapping into out.
			if keyNode != nil && keyNode.MergeKey {
				if err := mergeYAMLInto(out, valNode, anchors, seen); err != nil {
					return nil, err
				}
				continue
			}
			keyStr, err := yamlKeyString(keyNode, anchors, seen)
			if err != nil {
				return nil, err
			}
			val, err := convertYAMLNode(valNode, anchors, seen)
			if err != nil {
				return nil, err
			}
			out[keyStr] = val
		}
		return out, nil
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Children))
		for _, c := range n.Children {
			v, err := convertYAMLNode(c, anchors, seen)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case yaml.ScalarNode:
		return convertYAMLScalar(n), nil
	case yaml.DocumentNode:
		if len(n.Children) == 0 {
			return nil, nil //nolint:nilnil // empty document
		}
		return convertYAMLNode(n.Children[0], anchors, seen)
	default:
		return n.Value, nil
	}
}

func mergeYAMLInto(dst map[string]any, src *yaml.Node, anchors map[string]*yaml.Node, seen map[*yaml.Node]bool) error {
	if src == nil {
		return nil
	}
	v, err := convertYAMLNode(src, anchors, seen)
	if err != nil {
		return err
	}
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if _, present := dst[k]; !present {
				dst[k] = val
			}
		}
	case []any:
		for _, item := range t {
			m, ok := item.(map[string]any)
			if !ok {
				return fmt.Errorf("%w: merge sequence entry must be a mapping, got %T", ErrInvalidYAML, item)
			}
			for k, val := range m {
				if _, present := dst[k]; !present {
					dst[k] = val
				}
			}
		}
	default:
		return fmt.Errorf("%w: merge value must be a mapping or sequence of mappings, got %T", ErrInvalidYAML, v)
	}
	return nil
}

func yamlKeyString(n *yaml.Node, anchors map[string]*yaml.Node, seen map[*yaml.Node]bool) (string, error) {
	v, err := convertYAMLNode(n, anchors, seen)
	if err != nil {
		return "", err
	}
	switch t := v.(type) {
	case string:
		return t, nil
	case nil:
		return "", nil
	case bool:
		return strconv.FormatBool(t), nil
	case json.Number:
		return t.String(), nil
	default:
		return fmt.Sprintf("%v", t), nil
	}
}

// convertYAMLScalar mirrors the YAML core schema's plain-scalar resolution
// (yaml/decode.go's scalarToAnyCore) but emits [json.Number] for numeric
// values so number-precision keywords keep their full source text.
func convertYAMLScalar(n *yaml.Node) any {
	val := n.Value
	// Explicit string tag — value stays string, even if it lexes as a number.
	if n.Tag == "tag:yaml.org,2002:str" || n.Tag == "!" {
		return val
	}
	// Quoted scalars are always strings under the YAML core/json schemas.
	if n.Style != yaml.PlainStyle {
		return val
	}
	// Empty plain scalar => null.
	if val == "" {
		return nil
	}
	// Null literals.
	switch val {
	case "null", "Null", "NULL", "~":
		return nil
	}
	// Booleans (YAML 1.2 core schema).
	switch val {
	case "true", "True", "TRUE":
		return true
	case "false", "False", "FALSE":
		return false
	}
	if isYAMLNumber(val) {
		return json.Number(yamlNormalizeNumber(val))
	}
	return val
}

// isYAMLNumber reports whether val parses as a YAML 1.2 core-schema integer
// or float. Underscores are not allowed in YAML 1.2 numbers.
func isYAMLNumber(val string) bool {
	if val == "" {
		return false
	}
	// Special floats.
	switch val {
	case ".inf", ".Inf", ".INF", "+.inf", "+.Inf", "+.INF",
		"-.inf", "-.Inf", "-.INF",
		".nan", ".NaN", ".NAN":
		return true
	}
	// Integer (decimal, hex, octal).
	if _, err := strconv.ParseInt(val, 0, 64); err == nil {
		return true
	}
	if _, err := strconv.ParseUint(val, 0, 64); err == nil {
		return true
	}
	// Float.
	if _, err := strconv.ParseFloat(val, 64); err == nil {
		return true
	}
	return false
}

// yamlNormalizeNumber returns a number string suitable for [json.Number].
// Hex and octal literals are rewritten to base-10; scientific and decimal
// literals are passed through verbatim. .inf / .nan are not representable
// in JSON — we emit a float64 string ("Inf" / "NaN") which the validator's
// numeric comparators handle as float64; this is a documented limitation.
func yamlNormalizeNumber(val string) string {
	switch val {
	case ".inf", ".Inf", ".INF", "+.inf", "+.Inf", "+.INF":
		return "Inf"
	case "-.inf", "-.Inf", "-.INF":
		return "-Inf"
	case ".nan", ".NaN", ".NAN":
		return "NaN"
	}
	// Hex/octal — convert to base 10 so json.Number formatting matches JSON.
	if i, err := strconv.ParseInt(val, 0, 64); err == nil {
		// strconv.ParseInt with base 0 accepts plain decimal too — only
		// rewrite if the source actually had a base prefix.
		if hasBasePrefix(val) {
			return strconv.FormatInt(i, 10)
		}
		return val
	}
	if u, err := strconv.ParseUint(val, 0, 64); err == nil {
		if hasBasePrefix(val) {
			return strconv.FormatUint(u, 10)
		}
		return val
	}
	return val
}

func hasBasePrefix(s string) bool {
	t := s
	if strings.HasPrefix(t, "+") || strings.HasPrefix(t, "-") {
		t = t[1:]
	}
	return strings.HasPrefix(t, "0x") || strings.HasPrefix(t, "0X") ||
		strings.HasPrefix(t, "0o") || strings.HasPrefix(t, "0O") ||
		strings.HasPrefix(t, "0b") || strings.HasPrefix(t, "0B")
}

// decodeTOML walks the TOML AST and produces a map[string]any tree with
// numbers as [json.Number] and datetime values flattened to RFC3339-ish
// strings.
func decodeTOML(data []byte) (any, error) {
	file, err := toml.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse toml: %w", err)
	}
	if file.Root == nil {
		return map[string]any{}, nil
	}
	return convertTOMLTable(file.Root)
}

func convertTOMLTable(n *toml.Node) (map[string]any, error) {
	out := map[string]any{}
	for _, child := range n.Children {
		switch child.Kind {
		case toml.KeyValueNode:
			if len(child.Children) != 1 {
				return nil, fmt.Errorf("%w: key-value node has %d children, want 1", ErrInvalidTOML, len(child.Children))
			}
			v, err := convertTOMLValue(child.Children[0])
			if err != nil {
				return nil, err
			}
			if err := setTOMLNested(out, child.Key, v, false); err != nil {
				return nil, err
			}
		case toml.TableNode:
			sub, err := convertTOMLTable(child)
			if err != nil {
				return nil, err
			}
			if err := setTOMLNested(out, child.Key, sub, false); err != nil {
				return nil, err
			}
		case toml.ArrayTableNode:
			sub, err := convertTOMLTable(child)
			if err != nil {
				return nil, err
			}
			if err := setTOMLNested(out, child.Key, sub, true); err != nil {
				return nil, err
			}
		case toml.CommentNode:
			// ignored
		default:
			return nil, fmt.Errorf("%w: unexpected top-level node kind %v", ErrInvalidTOML, child.Kind)
		}
	}
	return out, nil
}

func setTOMLNested(out map[string]any, key []string, val any, asArrayElement bool) error {
	if len(key) == 0 {
		return fmt.Errorf("%w: empty key path", ErrInvalidTOML)
	}
	cur := out
	for i, seg := range key {
		last := i == len(key)-1
		if last {
			if asArrayElement {
				existing := cur[seg]
				switch arr := existing.(type) {
				case nil:
					cur[seg] = []any{val}
				case []any:
					cur[seg] = append(arr, val)
				default:
					return fmt.Errorf("%w: cannot append array-table to non-array at %q", ErrInvalidTOML, seg)
				}
			} else {
				if existing, present := cur[seg]; present {
					em, emOK := existing.(map[string]any)
					vm, vmOK := val.(map[string]any)
					if emOK && vmOK {
						maps.Copy(em, vm)
						return nil
					}
				}
				cur[seg] = val
			}
			return nil
		}
		next, present := cur[seg]
		if !present {
			nm := map[string]any{}
			cur[seg] = nm
			cur = nm
			continue
		}
		switch nm := next.(type) {
		case map[string]any:
			cur = nm
		case []any:
			if len(nm) == 0 {
				return fmt.Errorf("%w: cannot descend into empty array at %q", ErrInvalidTOML, seg)
			}
			tail, ok := nm[len(nm)-1].(map[string]any)
			if !ok {
				return fmt.Errorf("%w: cannot descend into non-table array at %q", ErrInvalidTOML, seg)
			}
			cur = tail
		default:
			return fmt.Errorf("%w: cannot descend into %T at %q", ErrInvalidTOML, next, seg)
		}
	}
	return nil
}

func convertTOMLValue(n *toml.Node) (any, error) {
	switch n.Kind {
	case toml.StringNode:
		return n.Value, nil
	case toml.IntegerNode:
		return tomlIntegerToNumber(n.Value)
	case toml.FloatNode:
		return tomlFloatToNumber(n.Value)
	case toml.BooleanNode:
		switch n.Value {
		case "true":
			return true, nil
		case "false":
			return false, nil
		default:
			return nil, fmt.Errorf("%w: invalid boolean literal %q", ErrInvalidTOML, n.Value)
		}
	case toml.DateTimeNode, toml.LocalDateTimeNode, toml.LocalDateNode, toml.LocalTimeNode:
		// Raw text is already RFC3339-shaped for offset/local datetimes,
		// or YYYY-MM-DD / HH:MM:SS for the local-* variants — emit it as
		// a string so {"format": "date-time" | "date" | "time"} works.
		return n.Value, nil
	case toml.ArrayNode:
		out := make([]any, 0, len(n.Children))
		for _, c := range n.Children {
			v, err := convertTOMLValue(c)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case toml.InlineTableNode:
		out := map[string]any{}
		for _, c := range n.Children {
			if c.Kind != toml.KeyValueNode {
				return nil, fmt.Errorf("%w: inline table child kind %v unexpected", ErrInvalidTOML, c.Kind)
			}
			if len(c.Children) != 1 {
				return nil, fmt.Errorf("%w: inline table key-value has %d children", ErrInvalidTOML, len(c.Children))
			}
			v, err := convertTOMLValue(c.Children[0])
			if err != nil {
				return nil, err
			}
			if err := setTOMLNested(out, c.Key, v, false); err != nil {
				return nil, err
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%w: unexpected value node kind %v", ErrInvalidTOML, n.Kind)
	}
}

func tomlIntegerToNumber(raw string) (json.Number, error) {
	clean := stripUnderscores(raw)
	// strconv.ParseInt with base 0 understands 0x / 0o / 0b prefixes and
	// signed decimal. Convert to canonical base-10 text for json.Number.
	if i, err := strconv.ParseInt(clean, 0, 64); err == nil {
		return json.Number(strconv.FormatInt(i, 10)), nil
	}
	// TOML allows up to 64-bit signed; if it overflows int64 try uint64
	// for unsigned hex literals like 0xFFFFFFFFFFFFFFFF.
	if u, err := strconv.ParseUint(clean, 0, 64); err == nil {
		return json.Number(strconv.FormatUint(u, 10)), nil
	}
	return "", fmt.Errorf("%w: integer %q out of range", ErrInvalidTOML, raw)
}

func tomlFloatToNumber(raw string) (json.Number, error) {
	clean := stripUnderscores(raw)
	switch clean {
	case "inf", "+inf":
		return json.Number("Inf"), nil
	case "-inf":
		return json.Number("-Inf"), nil
	case "nan", "+nan", "-nan":
		return json.Number("NaN"), nil
	}
	// Validate it actually parses as a float, but pass the text through
	// so we don't drop precision for things like 0.1 + 0.2-style tests.
	if _, err := strconv.ParseFloat(clean, 64); err != nil {
		return "", fmt.Errorf("%w: invalid float %q: %s", ErrInvalidTOML, raw, err.Error())
	}
	return json.Number(clean), nil
}

func stripUnderscores(s string) string {
	if !strings.Contains(s, "_") {
		return s
	}
	out := make([]byte, 0, len(s))
	for i := range len(s) {
		if s[i] != '_' {
			out = append(out, s[i])
		}
	}
	return string(out)
}
