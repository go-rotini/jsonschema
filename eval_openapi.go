package jsonschema

// This file registers the OpenAPI 3.1 base vocabulary keywords
// (`discriminator`, `xml`, `externalDocs`, `example`) as annotation-only
// evaluators. The OAS dialect ([OASDialectURL]) is a strict superset of
// Draft 2020-12 — these four keywords carry metadata for tooling and never
// produce validation errors. The package registers them unconditionally so
// that schemas declaring `$schema: "https://spec.openapis.org/oas/3.1/dialect/base"`
// (or the upstream Draft 2020-12 URL with OpenAPI extensions sprinkled in)
// compile cleanly without falling back to the strict-keyword warning path.

//nolint:gochecknoinits // evaluator registry is built at package init by design.
func init() {
	for _, n := range []string{
		"discriminator",
		"xml",
		"externalDocs",
		"example",
	} {
		name := n
		registerEvaluator(name, func(_ *evalBuilder, raw any, loc string) (evaluator, error) {
			return &annotationEval{loc: loc, kw: name, value: raw}, nil
		})
	}
}
