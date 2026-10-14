// Package httploader resolves JSON Schema $refs over HTTPS for
// [github.com/go-rotini/jsonschema].
//
// The jsonschema package itself never touches the network: its default
// loader serves only the embedded meta-schemas, and programs that import it
// do not link net/http. A program that needs remote refs opts in here:
//
//	schema, err := jsonschema.Compile(src, jsonschema.WithLoader(httploader.Default()))
//
// [Default] keeps meta-schema refs offline and fetches everything else over
// HTTPS. Build a [Loader] directly to set a client, a timeout, a body cap, a
// response cache, plain-http access, request headers or a refusal to connect
// to private addresses, and combine it with other loaders through
// [jsonschema.ChainLoader].
package httploader
