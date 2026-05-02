package jsonschema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// ValidateTo validates instanceJSON against schema and returns the decoded
// value of type T on success, or the validation error on failure. It is the
// generic counterpart to [*Schema.ValidateAndUnmarshal].
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
		if len(res.Errors) > 0 {
			e := res.Errors[0]
			return zero, &e
		}
		return zero, ErrValidationFailed
	}
	var v T
	if err := json.Unmarshal(instanceJSON, &v); err != nil {
		return zero, fmt.Errorf("jsonschema: decode after validate: %w", err)
	}
	return v, nil
}

// Validate validates instanceJSON against the schema and returns a [*Result].
func (s *Schema) Validate(instanceJSON []byte, opts ...Option) (*Result, error) {
	if s == nil {
		return nil, ErrSchemaNotCompiled
	}
	value, err := decodeInstanceBytes(instanceJSON)
	if err != nil {
		return nil, err
	}
	return s.ValidateValue(value, opts...)
}

// ValidateValue validates an already-decoded Go value against the schema.
func (s *Schema) ValidateValue(v any, opts ...Option) (*Result, error) {
	if s == nil {
		return nil, ErrSchemaNotCompiled
	}
	ro := defaultRunOptions()
	for _, o := range opts {
		o(ro)
	}
	ctx := newRunCtx(s, ro)
	defer ctx.release()
	if root := s.evalRoot(); root != nil {
		ctx.evaluate(root, v)
	}
	res := &Result{Valid: len(ctx.errors) == 0, Errors: ctx.errors}
	if ro.collectAnnotations {
		res.Annotations = ctx.publicAnnotations()
	}
	return res, nil
}

// ValidateReader streams instance bytes from r and validates them against
// the schema.
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

// ValidateAndUnmarshal validates instanceJSON, then (on success) decodes it
// into v.
func (s *Schema) ValidateAndUnmarshal(instanceJSON []byte, v any, opts ...Option) error {
	if s == nil {
		return ErrSchemaNotCompiled
	}
	res, err := s.Validate(instanceJSON, opts...)
	if err != nil {
		return err
	}
	if !res.Valid {
		if len(res.Errors) > 0 {
			e := res.Errors[0]
			return &e
		}
		return ErrValidationFailed
	}
	if v == nil {
		return nil
	}
	if err := json.Unmarshal(instanceJSON, v); err != nil {
		return fmt.Errorf("jsonschema: decode after validate: %w", err)
	}
	return nil
}

// decodeInstanceBytes decodes raw JSON bytes via [encoding/json.Decoder] with
// UseNumber set so number-precision keywords like multipleOf can compare
// against the wire form.
func decodeInstanceBytes(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("jsonschema: decode instance: %w", err)
	}
	return v, nil
}
