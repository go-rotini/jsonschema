package jsonschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ValidateTo validates instanceJSON against schema and, on success, decodes
// it into a value of type T with [encoding/json.Unmarshal]. It is the generic
// counterpart to [*Schema.ValidateAndUnmarshal] and reports failures the
// same way.
func ValidateTo[T any](schema *Schema, instanceJSON []byte, opts ...Option) (T, error) {
	var zero T
	if schema == nil {
		return zero, ErrSchemaNotCompiled
	}
	res, err := schema.Validate(instanceJSON, opts...)
	if err != nil {
		return zero, err
	}
	if !res.Valid {
		return zero, validationFailureError(res.Errors)
	}
	var v T
	if err := json.Unmarshal(instanceJSON, &v); err != nil {
		return zero, fmt.Errorf("jsonschema: decode after validate: %w", err)
	}
	return v, nil
}

// MustValidateTo is the panic-on-error variant of [ValidateTo], for static,
// known-good instances where a failure is a programming error.
func MustValidateTo[T any](schema *Schema, instanceJSON []byte, opts ...Option) T {
	v, err := ValidateTo[T](schema, instanceJSON, opts...)
	if err != nil {
		panic(err)
	}
	return v
}

// Validate validates instanceJSON against the schema and returns a [*Result].
// The returned error reports input that could not be validated (not valid
// JSON, trailing content after the document, or over [WithMaxInstanceSize]);
// a schema violation is reported in the Result, not as an error.
//
// Empty input ([]byte{} or all-whitespace) is rejected with an error wrapping
// [io.EOF]. The literal `null` is a valid JSON value and validates against
// any schema that accepts the null type; it is never treated as "no input".
func (s *Schema) Validate(instanceJSON []byte, opts ...Option) (*Result, error) {
	if s == nil {
		return nil, ErrSchemaNotCompiled
	}
	ro := defaultRunOptions()
	for _, o := range opts {
		o(ro)
	}
	if ro.maxInstanceSize > 0 && len(instanceJSON) > ro.maxInstanceSize {
		return nil, ErrInstanceTooLarge
	}
	value, err := decodeInstanceBytes(instanceJSON)
	if err != nil {
		return nil, err
	}
	return s.validateWithOptions(value, ro)
}

// ValidateValue validates an already-decoded Go value against the schema.
// v must use the shapes [encoding/json.Unmarshal] produces into an any:
// map[string]any, []any, string, bool, nil, and float64 or [Number]. Every
// Go integer kind, float32 and a non-nil *[math/big.Int] are also accepted
// as numbers, so a uint64 above the int64 range is an integer; an integer
// beyond uint64 can be passed as a [Number] holding its digits. Other
// types, such as structs, typed slices or [MapSlice], match no JSON type.
func (s *Schema) ValidateValue(v any, opts ...Option) (*Result, error) {
	if s == nil {
		return nil, ErrSchemaNotCompiled
	}
	ro := defaultRunOptions()
	for _, o := range opts {
		o(ro)
	}
	return s.validateWithOptions(v, ro)
}

// validateWithOptions is the inner entry point shared by [*Schema.Validate]
// and [*Schema.ValidateValue]; ro is the already-resolved option set.
func (s *Schema) validateWithOptions(v any, ro *runOptions) (*Result, error) {
	ctx := newRunCtx(s, ro)
	defer ctx.release()
	ctx.setEvalLimit(v)
	if root := s.evalRoot(); root != nil {
		ctx.evaluate(root, v)
	}
	if ctx.overBudget {
		return nil, ErrMaxEvaluations
	}
	res := &Result{Valid: len(ctx.errors) == 0, Errors: ctx.errors}
	// Stop-on-first-error skips annotation collection: unevaluated* keywords
	// and the Detailed/Verbose output formats are not useful when the
	// validator short-circuits on the first failure.
	if ro.collectAnnotations && !ro.stopOnFirstError {
		res.Annotations = ctx.publicAnnotations()
	}
	return res, nil
}

// ValidateReader reads all of r and validates the bytes as
// [*Schema.Validate] does. A nil r returns [ErrNilReader].
func (s *Schema) ValidateReader(r io.Reader, opts ...Option) (*Result, error) {
	if s == nil {
		return nil, ErrSchemaNotCompiled
	}
	if r == nil {
		return nil, ErrNilReader
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: read instance: %w", err)
	}
	return s.Validate(data, opts...)
}

// ValidateAndUnmarshal validates instanceJSON and, on success, decodes it
// into v with [encoding/json.Unmarshal] (skipped when v is nil). When the
// instance is invalid it returns the first [*ValidationError], with the
// remaining failures appended to its Causes.
func (s *Schema) ValidateAndUnmarshal(instanceJSON []byte, v any, opts ...Option) error {
	if s == nil {
		return ErrSchemaNotCompiled
	}
	res, err := s.Validate(instanceJSON, opts...)
	if err != nil {
		return err
	}
	if !res.Valid {
		return validationFailureError(res.Errors)
	}
	if v == nil {
		return nil
	}
	if err := json.Unmarshal(instanceJSON, v); err != nil {
		return fmt.Errorf("jsonschema: decode after validate: %w", err)
	}
	return nil
}

// validationFailureError packages a slice of failures into a single
// [*ValidationError]. The first failure becomes the head; the rest are
// attached as Causes so [errors.As] can recover the full slice.
func validationFailureError(errs []ValidationError) error {
	if len(errs) == 0 {
		return ErrValidationFailed
	}
	if len(errs) == 1 {
		e := errs[0]
		return &e
	}
	first := errs[0]
	first.Causes = append(first.Causes, errs[1:]...)
	return &first
}

// decodeInstanceBytes decodes raw JSON bytes with UseNumber set so
// precision-sensitive keywords compare against the wire form. Trailing
// non-whitespace is rejected to block concatenated-document smuggling.
func decodeInstanceBytes(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("jsonschema: decode instance: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err == nil {
		return nil, fmt.Errorf("jsonschema: decode instance: %w", errTrailingContent)
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("jsonschema: decode instance: %w", err)
	}
	return v, nil
}
