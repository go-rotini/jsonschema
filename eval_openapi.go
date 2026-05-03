package jsonschema

// Registers the OpenAPI 3.1 base vocabulary keywords (discriminator, xml,
// externalDocs, example) as annotation-only evaluators so schemas using
// the OAS dialect ([OASDialectURL]) compile cleanly under strict-keyword
// mode without producing validation errors.

//nolint:gochecknoinits // evaluator registry is built at package init by design.
func init() {
	for _, n := range []string{
		"discriminator",
		"xml",
		"externalDocs",
		"example",
	} {
		name := n
		registerEvaluator(name, func(_ *evalBuilder, _ *buildFrame, raw any, loc string) (evaluator, error) {
			return &annotationEval{loc: loc, kw: name, value: raw}, nil
		})
	}
}
