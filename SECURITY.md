# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability, please report it responsibly by emailing **matthewcgetz@gmail.com**. Do not open a public issue.

You should receive a response within 72 hours. If accepted, a fix will be developed privately and released as a patch version.

## Resource Limits

This package defaults to safe behavior to mitigate denial-of-service attacks:

- **Reference depth** is bounded via `WithMaxRefDepth` to prevent runaway `$ref` chains.
- **Evaluation count** is bounded via `WithMaxEvaluations`. A schema whose `$ref`s branch, such as `{"$defs":{"d":{"allOf":[{"$ref":"#/$defs/d"},{"$ref":"#/$defs/d"}]}},"$ref":"#/$defs/d"}`, would otherwise take time exponential in its depth. By default one validation may evaluate 100 subschemas per value in the instance, and at least 1,000,000; errors count against the same budget by their size, so it also bounds memory. Exceeding it makes `Validate` return `ErrMaxEvaluations`.
- **Validation depth** is bounded to prevent stack exhaustion.
- **Instance size** can be capped via `WithMaxDocumentSize` (alias `WithMaxInstanceSize`) to bound the bytes accepted from any single instance before parsing.
- **Numbers** are compared exactly without expanding their exponents, so a value such as `1e999999999` costs no more than `1` and is still judged by `maximum`, `multipleOf` and the rest.
- **`uniqueItems`** runs in time linear in the array's size, NaN items included.
- **`multipleOf`** runs in time linear in the instance number's digits.
- **YAML aliases** in `LoadYAML` and `ValidateYAML` may expand a document to at most 100 times its parsed nodes (and at least 10,000), which stops "billion laughs" documents.
- **Remote documents** are fetched while compiling, never while validating, and a `$ref` that cannot be resolved fails `Compile`.
- **Remote loading** is off by default: the default loader serves only the embedded meta-schemas and never touches the network. Fetching remote `$ref`s is opt-in through the `httploader` subpackage, which is HTTPS-only unless `AllowHTTP` is set; reading `file://` refs is opt-in through `FileLoader`.

These limits can be configured via compile and validate options but are set to safe defaults out of the box.

### Validating against untrusted schemas

A schema can be written to cost a lot to evaluate. When annotations are collected (the default), `anyOf` evaluates every branch, as the specification requires, so `anyOf` nested through `$ref`s can take time exponential in the nesting depth. Validate against untrusted schemas with `WithCollectAnnotations(false)`: `anyOf` then stops at the first passing branch unless an `unevaluatedProperties` or `unevaluatedItems` keyword needs the others. Branching `$ref`s cost time whether or not annotations are collected; the evaluation budget stops them, and `WithMaxEvaluations` with a smaller value, together with `WithMaxInstanceSize`, sets how much one validation may cost.

### Fetching untrusted schemas over HTTP

A schema can name any URL in a `$ref`, so compiling an untrusted schema with an `httploader.Loader` lets that schema make your program fetch internal addresses: loopback services, private networks or cloud metadata endpoints. The loader refuses an https-to-http redirect unless `AllowHTTP` is set and strips `RequestDecorator` headers when a redirect changes scheme or host. Set `DenyPrivate` to refuse loopback, private, link-local, shared-address and unspecified IP addresses; the check runs on the address actually dialed, so it also covers redirects and DNS answers, and an IPv6 address that embeds an IPv4 one (NAT64, 6to4) is judged by the IPv4 address. It needs the built-in client: with a custom `Client` every fetch fails. With a proxy configured it checks the proxy's address.
