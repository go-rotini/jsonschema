# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability, please report it responsibly by emailing **matthewcgetz@gmail.com**. Do not open a public issue.

You should receive a response within 72 hours. If accepted, a fix will be developed privately and released as a patch version.

## Resource Limits

This package defaults to safe behavior to mitigate denial-of-service attacks:

- **Reference depth** is bounded via `WithMaxRefDepth` to prevent runaway `$ref` chains.
- **Validation depth** is bounded to prevent stack exhaustion.
- **Instance size** can be capped via `WithMaxDocumentSize` (alias `WithMaxInstanceSize`) to bound the bytes accepted from any single instance before parsing.
- **Remote loading** is off by default: the default loader serves only the embedded meta-schemas and never touches the network. Fetching remote `$ref`s is opt-in through the `httploader` subpackage, which is HTTPS-only unless `AllowHTTP` is set; reading `file://` refs is opt-in through `FileLoader`.

These limits can be configured via compile and validate options but are set to safe defaults out of the box.
