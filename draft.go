package jsonschema

import "strings"

// Draft enumerates the JSON Schema specification drafts the package
// recognizes. Use [DraftDefault] when you need the package's preferred draft;
// pin to a specific constant when interacting with legacy schemas.
type Draft int

// Recognized drafts.
const (
	// DraftUnknown is the zero value; it indicates "no draft selected".
	DraftUnknown Draft = iota
	// Draft4 corresponds to JSON Schema Draft 4 (2013).
	Draft4
	// Draft6 corresponds to JSON Schema Draft 6 (2017).
	Draft6
	// Draft7 corresponds to JSON Schema Draft 7 (2018).
	Draft7
	// Draft201909 corresponds to JSON Schema Draft 2019-09.
	Draft201909
	// Draft202012 corresponds to JSON Schema Draft 2020-12.
	Draft202012
)

// DraftDefault is the draft used when neither the schema's $schema keyword
// nor the caller's [WithDefaultDraft] specifies one.
const DraftDefault = Draft202012

// String returns a human-readable label for d (e.g. "Draft 2020-12").
func (d Draft) String() string {
	switch d {
	case Draft4:
		return "Draft 4"
	case Draft6:
		return "Draft 6"
	case Draft7:
		return "Draft 7"
	case Draft201909:
		return "Draft 2019-09"
	case Draft202012:
		return "Draft 2020-12"
	case DraftUnknown:
		return "Draft Unknown"
	default:
		return "Draft Unknown"
	}
}

// MetaSchemaURL returns the canonical URL of the meta-schema that defines d.
// Legacy drafts (4/6/7) historically used http://; the modern drafts use https://.
// Returns the empty string for [DraftUnknown].
func (d Draft) MetaSchemaURL() string {
	switch d {
	case Draft4:
		return "http://json-schema.org/draft-04/schema#"
	case Draft6:
		return "http://json-schema.org/draft-06/schema#"
	case Draft7:
		return "http://json-schema.org/draft-07/schema#"
	case Draft201909:
		return "https://json-schema.org/draft/2019-09/schema"
	case Draft202012:
		return "https://json-schema.org/draft/2020-12/schema"
	case DraftUnknown:
		return ""
	default:
		return ""
	}
}

// IDKeyword returns the schema-identifier keyword name for d. Draft 4 spelled
// it as "id" (no leading $); subsequent drafts use "$id".
func (d Draft) IDKeyword() string {
	if d == Draft4 {
		return "id"
	}
	return "$id"
}

// DefsKeyword returns the subschema-container keyword name for d. Drafts 4
// through 7 used "definitions"; Draft 2019-09 introduced "$defs".
func (d Draft) DefsKeyword() string {
	switch d {
	case Draft4, Draft6, Draft7:
		return "definitions"
	default:
		return "$defs"
	}
}

// DraftFromMetaSchemaURL maps a meta-schema URL back to a [Draft] constant.
// Both http:// and https:// variants of the legacy URLs are accepted, and
// trailing # fragments are tolerated. Returns [DraftUnknown] when no draft
// matches.
func DraftFromMetaSchemaURL(url string) Draft {
	canon := canonicalizeMetaSchemaURL(url)
	switch canon {
	case "json-schema.org/draft-04/schema":
		return Draft4
	case "json-schema.org/draft-06/schema":
		return Draft6
	case "json-schema.org/draft-07/schema":
		return Draft7
	case "json-schema.org/draft/2019-09/schema":
		return Draft201909
	case "json-schema.org/draft/2020-12/schema":
		return Draft202012
	default:
		return DraftUnknown
	}
}

// canonicalizeMetaSchemaURL strips the scheme and a trailing # from u so the
// switch in [DraftFromMetaSchemaURL] can do a single equality check per draft
// across http/https + with-or-without-fragment input variants.
func canonicalizeMetaSchemaURL(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimSuffix(u, "#")
	return u
}
