package jsonschema

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"regexp/syntax"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// FormatValidator validates a string against a named format. It returns nil
// when the value conforms; otherwise it returns a non-nil error (typically a
// [*FormatError] but any error is accepted from custom validators).
type FormatValidator func(string) error

// builtinFormats holds every format validator the package ships with. Custom
// formats registered via [WithCustomFormat] win when both are defined for a
// name (per spec §7.2.1).
var builtinFormats = map[string]FormatValidator{
	"date-time":             validateDateTime,
	"date":                  validateDate,
	"time":                  validateTime,
	"duration":              validateDuration,
	"uuid":                  validateUUID,
	"uri":                   validateURI,
	"uri-reference":         validateURIReference,
	"iri":                   validateIRI,
	"iri-reference":         validateIRIReference,
	"uri-template":          validateURITemplate,
	"json-pointer":          validateJSONPointer,
	"relative-json-pointer": validateRelativeJSONPointer,
	"ipv4":                  validateIPv4,
	"ipv6":                  validateIPv6,
	"hostname":              validateHostname,
	"idn-hostname":          validateIDNHostname,
	"email":                 validateEmail,
	"idn-email":             validateIDNEmail,
	"regex":                 validateRegex,
}

// lookupFormat returns the validator registered for name, consulting custom
// formats first and falling back to the built-in table. The second return is
// false when the name is unknown.
func lookupFormat(name string, custom map[string]func(string) error) (FormatValidator, bool) {
	if fn, ok := custom[name]; ok {
		return fn, true
	}
	fn, ok := builtinFormats[name]
	return fn, ok
}

// errFormatInvalid is the sentinel underlying every built-in format failure.
// Callers can errors.Is(err, errFormatInvalid) to recognize a format reject
// without unpacking the *FormatError wrapper.
var errFormatInvalid = errors.New("invalid format value")

// formatErr returns a *FormatError describing why value failed format,
// wrapping errFormatInvalid (with the formatted detail) as the cause so the
// linters' static-error rule is satisfied.
func formatErr(format, value, detail string) error {
	return &FormatError{
		Format: format,
		Value:  value,
		Cause:  fmt.Errorf("%w: %s", errFormatInvalid, detail),
	}
}

// formatErrCause wraps an existing error as a FormatError cause.
func formatErrCause(format, value string, cause error) error {
	return &FormatError{Format: format, Value: value, Cause: cause}
}

// validateDateTime accepts an RFC 3339 date-time. Both `Z` and numeric
// offsets are tolerated; leap seconds and fractional seconds are honored.
func validateDateTime(s string) error {
	// Validate structure ourselves (Go's time.Parse is too permissive on
	// offset bounds — it accepts -24:00 and +10:60). Then defer to time.Parse
	// for the date-portion calendar arithmetic.
	if len(s) < 20 {
		return formatErr("date-time", s, "too short")
	}
	if s[10] != 'T' && s[10] != 't' {
		return formatErr("date-time", s, "missing T separator")
	}
	if _, err := time.Parse("2006-01-02", s[:10]); err != nil {
		return formatErrCause("date-time", s, err)
	}
	if !isFullTime(s[11:]) {
		return formatErr("date-time", s, "invalid time component")
	}
	return nil
}

// validateDate accepts an RFC 3339 full-date (YYYY-MM-DD).
func validateDate(s string) error {
	if !isFullDate(s) {
		return formatErr("date", s, "not RFC 3339 full-date")
	}
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return formatErrCause("date", s, err)
	}
	return nil
}

func isFullDate(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	for i, c := range s {
		switch i {
		case 4, 7:
			continue
		default:
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

// validateTime accepts an RFC 3339 full-time.
func validateTime(s string) error {
	if !isFullTime(s) {
		return formatErr("time", s, "not RFC 3339 full-time")
	}
	return nil
}

// isFullTime parses an RFC 3339 partial-time + offset, accepting leap seconds
// only when the hour/minute pair makes the leap valid for the offset shift.
//
//nolint:cyclop,gocyclo // straightforward grammar walker; splitting hurts readability.
func isFullTime(s string) bool {
	// HH:MM:SS[.frac](Z|+HH:MM|-HH:MM)
	if len(s) < 9 {
		return false
	}
	if s[2] != ':' || s[5] != ':' {
		return false
	}
	hour, err1 := strconv.Atoi(s[0:2])
	minute, err2 := strconv.Atoi(s[3:5])
	second, err3 := strconv.Atoi(s[6:8])
	if err1 != nil || err2 != nil || err3 != nil {
		return false
	}
	if hour > 23 || minute > 59 || second > 60 {
		return false
	}
	rest := s[8:]
	// Optional fractional seconds.
	if rest != "" && rest[0] == '.' {
		i := 1
		for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
			i++
		}
		if i == 1 {
			return false
		}
		rest = rest[i:]
	}
	if rest == "" {
		return false
	}
	// Offset.
	var offHour, offMinute, offSign int
	switch rest[0] {
	case 'Z', 'z':
		if len(rest) != 1 {
			return false
		}
	case '+', '-':
		if len(rest) != 6 || rest[3] != ':' {
			return false
		}
		var err error
		offHour, err = strconv.Atoi(rest[1:3])
		if err != nil {
			return false
		}
		offMinute, err = strconv.Atoi(rest[4:6])
		if err != nil {
			return false
		}
		if offHour > 23 || offMinute > 59 {
			return false
		}
		if rest[0] == '-' {
			offSign = -1
		} else {
			offSign = 1
		}
	default:
		return false
	}
	if second == 60 {
		// Compute the local minute-of-day shifted into UTC and check it
		// equals 23:59 UTC.
		shift := offSign * (offHour*60 + offMinute)
		utcMinutes := (hour*60 + minute) - shift
		utcMinutes = ((utcMinutes % 1440) + 1440) % 1440
		if utcMinutes != 23*60+59 {
			return false
		}
	}
	return true
}

// validateDuration accepts ISO 8601 duration syntax per RFC 3339 Appendix A.
// The grammar enforces hierarchical containment: Y → M → D in the date
// portion, H → M → S in the time portion. Skipping a level (e.g. P1Y2D) is
// invalid.
func validateDuration(s string) error {
	if len(s) < 2 || s[0] != 'P' {
		return formatErr("duration", s, "must start with P")
	}
	rest := s[1:]
	if rest == "" {
		return formatErr("duration", s, "empty duration")
	}
	if strings.HasSuffix(rest, "W") {
		num := rest[:len(rest)-1]
		if num == "" {
			return formatErr("duration", s, "empty week count")
		}
		for _, c := range num {
			if c < '0' || c > '9' {
				return formatErr("duration", s, "non-digit in week count")
			}
		}
		return nil
	}
	if err := parseDurationDateTime(rest); err != nil {
		return formatErrCause("duration", s, err)
	}
	return nil
}

// errBadDuration is the sentinel for malformed duration components.
var errBadDuration = errors.New("bad duration")

// parseDurationDateTime parses dur-date and optional dur-time per RFC 3339
// Appendix A.
func parseDurationDateTime(s string) error {
	i, sawAny, err := parseDurationDate(s)
	if err != nil {
		return err
	}
	if i < len(s) && s[i] == 'T' {
		gotTime, err := parseDurationTime(s[i+1:])
		if err != nil {
			return err
		}
		sawAny = sawAny || gotTime
	}
	if !sawAny {
		return fmt.Errorf("%w: no components", errBadDuration)
	}
	return nil
}

// parseDurationDate consumes the YMD portion. Returns the cursor index after
// the consumed prefix, whether any unit was seen, or an error.
func parseDurationDate(s string) (int, bool, error) {
	i := 0
	dateOrder := "YMD"
	dateNext := -1
	saw := false
	for i < len(s) && s[i] != 'T' {
		j := i
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j == i {
			return i, saw, fmt.Errorf("%w: designator without digits", errBadDuration)
		}
		if j >= len(s) {
			return i, saw, fmt.Errorf("%w: trailing digits in date", errBadDuration)
		}
		unit := s[j]
		pos := strings.IndexByte(dateOrder, unit)
		if pos < 0 {
			return i, saw, fmt.Errorf("%w: invalid date designator %c", errBadDuration, unit)
		}
		if dateNext != -1 && pos != dateNext {
			return i, saw, fmt.Errorf("%w: date designator %c violates Y/M/D hierarchy", errBadDuration, unit)
		}
		dateNext = pos + 1
		saw = true
		i = j + 1
	}
	return i, saw, nil
}

// parseDurationTime consumes the HMS portion (s is the substring AFTER T).
func parseDurationTime(s string) (bool, error) {
	timeOrder := "HMS"
	timeNext := -1
	i := 0
	saw := false
	for i < len(s) {
		j := i
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j == i {
			return saw, fmt.Errorf("%w: designator without digits", errBadDuration)
		}
		if j >= len(s) {
			return saw, fmt.Errorf("%w: trailing digits in time", errBadDuration)
		}
		unit := s[j]
		pos := strings.IndexByte(timeOrder, unit)
		if pos < 0 {
			return saw, fmt.Errorf("%w: invalid time designator %c", errBadDuration, unit)
		}
		if timeNext != -1 && pos != timeNext {
			return saw, fmt.Errorf("%w: time designator %c violates H/M/S hierarchy", errBadDuration, unit)
		}
		timeNext = pos + 1
		saw = true
		i = j + 1
	}
	if !saw {
		return saw, fmt.Errorf("%w: T without time component", errBadDuration)
	}
	return saw, nil
}

// validateUUID accepts the canonical RFC 4122 hex form 8-4-4-4-12.
func validateUUID(s string) error {
	if len(s) != 36 {
		return formatErr("uuid", s, "length not 36")
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return formatErr("uuid", s, "missing hyphen")
			}
		default:
			if !isHex(byte(c)) {
				return formatErr("uuid", s, "non-hex character")
			}
		}
	}
	return nil
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// validateURI accepts an RFC 3986 absolute URI: must have a scheme.
func validateURI(s string) error {
	if !isASCII(s) {
		return formatErr("uri", s, "non-ASCII; use iri")
	}
	if !isURIRefChars(s) {
		return formatErr("uri", s, "invalid character")
	}
	if !validPercentEncoding(s) {
		return formatErr("uri", s, "bad percent-encoding")
	}
	colon := strings.IndexByte(s, ':')
	if colon <= 0 {
		return formatErr("uri", s, "missing scheme")
	}
	if !isValidURIScheme(s[:colon]) {
		return formatErr("uri", s, "invalid scheme")
	}
	u, err := url.Parse(s)
	if err != nil {
		return formatErrCause("uri", s, err)
	}
	if u.Scheme == "" {
		return formatErr("uri", s, "missing scheme")
	}
	return nil
}

// validateURIReference accepts any URI-reference (relative or absolute).
func validateURIReference(s string) error {
	if !isASCII(s) {
		return formatErr("uri-reference", s, "non-ASCII; use iri-reference")
	}
	if !isURIRefChars(s) {
		return formatErr("uri-reference", s, "invalid character")
	}
	if !validPercentEncoding(s) {
		return formatErr("uri-reference", s, "bad percent-encoding")
	}
	if _, err := url.Parse(s); err != nil {
		return formatErrCause("uri-reference", s, err)
	}
	return nil
}

// isURIRefChars reports whether every character is in the RFC 3986
// uri-reference character set: ALPHA / DIGIT / unreserved / reserved / pct.
func isURIRefChars(s string) bool {
	for i := range len(s) {
		c := s[i]
		switch {
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
		case c >= '0' && c <= '9':
		case strings.IndexByte("-._~:/?#[]@!$&'()*+,;=%", c) >= 0:
		default:
			return false
		}
	}
	return true
}

// validPercentEncoding checks that every '%' is followed by two hex digits.
func validPercentEncoding(s string) bool {
	for i := range len(s) {
		if s[i] == '%' {
			if i+2 >= len(s) || !isHex(s[i+1]) || !isHex(s[i+2]) {
				return false
			}
		}
	}
	return true
}

// validateIRI accepts an RFC 3987 IRI. The structural pass allows non-ASCII
// (ucschar) but rejects bare control characters, backslashes, and other
// ASCII bytes outside the URI character set.
func validateIRI(s string) error {
	if containsCtrl(s) {
		return formatErr("iri", s, "control character")
	}
	if !isIRIRefChars(s) {
		return formatErr("iri", s, "invalid character")
	}
	if !validPercentEncoding(s) {
		return formatErr("iri", s, "bad percent-encoding")
	}
	colon := strings.IndexByte(s, ':')
	if colon <= 0 {
		return formatErr("iri", s, "missing scheme")
	}
	if !isValidURIScheme(s[:colon]) {
		return formatErr("iri", s, "invalid scheme")
	}
	if !checkIRIAuthority(s[colon+1:]) {
		return formatErr("iri", s, "bad authority")
	}
	if _, err := url.Parse(s); err != nil {
		return formatErrCause("iri", s, err)
	}
	return nil
}

// validateIRIReference accepts an RFC 3987 IRI-reference (relative or
// absolute). Backslashes / control bytes are rejected; non-ASCII passes.
func validateIRIReference(s string) error {
	if containsCtrl(s) {
		return formatErr("iri-reference", s, "control character")
	}
	if !isIRIRefChars(s) {
		return formatErr("iri-reference", s, "invalid character")
	}
	if !validPercentEncoding(s) {
		return formatErr("iri-reference", s, "bad percent-encoding")
	}
	if _, err := url.Parse(s); err != nil {
		return formatErrCause("iri-reference", s, err)
	}
	return nil
}

// isIRIRefChars allows the URI-reference char set plus any non-ASCII (ucschar
// from RFC 3987).
func isIRIRefChars(s string) bool {
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= 0x80:
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
		case c >= '0' && c <= '9':
		case strings.IndexByte("-._~:/?#[]@!$&'()*+,;=%", c) >= 0:
		default:
			return false
		}
	}
	return true
}

// checkIRIAuthority is a light check for malformed authority components,
// notably an unbracketed IPv6 host.
func checkIRIAuthority(rest string) bool {
	if !strings.HasPrefix(rest, "//") {
		return true
	}
	auth := rest[2:]
	if i := strings.IndexAny(auth, "/?#"); i >= 0 {
		auth = auth[:i]
	}
	if at := strings.LastIndexByte(auth, '@'); at >= 0 {
		auth = auth[at+1:]
	}
	if !strings.HasPrefix(auth, "[") && strings.Count(auth, ":") > 1 {
		return false
	}
	return true
}

// isValidURIScheme matches RFC 3986 ALPHA *( ALPHA / DIGIT / "+" / "-" / "." ).
func isValidURIScheme(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if i == 0 {
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
				return false
			}
			continue
		}
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') &&
			(c < '0' || c > '9') && c != '+' && c != '-' && c != '.' {
			return false
		}
	}
	return true
}

// containsCtrl reports whether s contains a control character (excluding
// horizontal tab) or surrogate code points.
func containsCtrl(s string) bool {
	for _, r := range s {
		if r == 0 || (r < 0x20 && r != '\t') || r == 0x7F {
			return true
		}
		if r >= 0xD800 && r <= 0xDFFF {
			return true
		}
	}
	return false
}

// isASCII reports whether every byte in s is < 0x80.
func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// validateURITemplate accepts an RFC 6570 URI Template (level 1-4).
func validateURITemplate(s string) error {
	i := 0
	for i < len(s) {
		c := s[i]
		switch c {
		case '{':
			j := strings.IndexByte(s[i+1:], '}')
			if j < 0 {
				return formatErr("uri-template", s, "unmatched {")
			}
			expr := s[i+1 : i+1+j]
			if !isURITemplateExpr(expr) {
				return formatErr("uri-template", s, "invalid expression")
			}
			i += j + 2
		case '}':
			return formatErr("uri-template", s, "unmatched }")
		case '%':
			if i+2 >= len(s) || !isHex(s[i+1]) || !isHex(s[i+2]) {
				return formatErr("uri-template", s, "bad percent-encoding")
			}
			i += 3
		default:
			if !isURITemplateLiteral(rune(c)) && c < 0x80 {
				return formatErr("uri-template", s, fmt.Sprintf("invalid literal %q", c))
			}
			if c >= 0x80 {
				_, sz := utf8.DecodeRuneInString(s[i:])
				if sz <= 1 {
					return formatErr("uri-template", s, "bad UTF-8")
				}
				i += sz
				continue
			}
			i++
		}
	}
	return nil
}

func isURITemplateLiteral(r rune) bool {
	switch r {
	case '!', '#', '$', '&', '(', ')', '*', '+', ',', '-', '.', '/', ':', ';', '=', '?', '@', '[', ']', '_', '~', '\'':
		return true
	}
	if r >= '0' && r <= '9' {
		return true
	}
	if r >= 'A' && r <= 'Z' {
		return true
	}
	if r >= 'a' && r <= 'z' {
		return true
	}
	return false
}

func isURITemplateExpr(s string) bool {
	if s == "" {
		return false
	}
	switch s[0] {
	case '+', '#', '.', '/', ';', '?', '&', '=', ',', '!', '@', '|':
		s = s[1:]
	}
	if s == "" {
		return false
	}
	for part := range strings.SplitSeq(s, ",") {
		if !isURITemplateVarspec(part) {
			return false
		}
	}
	return true
}

func isURITemplateVarspec(s string) bool {
	if strings.HasSuffix(s, "*") {
		s = s[:len(s)-1]
	} else if i := strings.IndexByte(s, ':'); i >= 0 {
		maxLen := s[i+1:]
		if maxLen == "" || len(maxLen) > 4 {
			return false
		}
		for _, c := range maxLen {
			if c < '0' || c > '9' {
				return false
			}
		}
		s = s[:i]
	}
	if s == "" {
		return false
	}
	for seg := range strings.SplitSeq(s, ".") {
		if seg == "" {
			return false
		}
		if !isURITemplateVarchars(seg) {
			return false
		}
	}
	return true
}

func isURITemplateVarchars(s string) bool {
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == '_':
			i++
		case c == '%':
			if i+2 >= len(s) || !isHex(s[i+1]) || !isHex(s[i+2]) {
				return false
			}
			i += 3
		case (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z'):
			i++
		default:
			return false
		}
	}
	return true
}

// validateJSONPointer accepts RFC 6901 JSON Pointer syntax.
func validateJSONPointer(s string) error {
	if s == "" {
		return nil
	}
	if s[0] != '/' {
		return formatErr("json-pointer", s, "must start with /")
	}
	for i := 1; i < len(s); i++ {
		if s[i] == '~' {
			if i+1 >= len(s) || (s[i+1] != '0' && s[i+1] != '1') {
				return formatErr("json-pointer", s, "bad ~ escape")
			}
			i++
		}
	}
	return nil
}

// validateRelativeJSONPointer accepts the Draft 2020-12 relative-json-pointer
// shape: non-negative-integer ('#' | json-pointer).
func validateRelativeJSONPointer(s string) error {
	if s == "" {
		return formatErr("relative-json-pointer", s, "empty")
	}
	i := 0
	if s[i] < '0' || s[i] > '9' {
		return formatErr("relative-json-pointer", s, "missing prefix integer")
	}
	if s[i] == '0' {
		i++
		if i < len(s) && s[i] >= '0' && s[i] <= '9' {
			return formatErr("relative-json-pointer", s, "leading zero")
		}
	} else {
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	}
	rest := s[i:]
	if rest == "#" {
		return nil
	}
	return validateJSONPointer(rest)
}

// validateIPv4 accepts a dotted-quad address.
func validateIPv4(s string) error {
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return formatErrCause("ipv4", s, err)
	}
	if !addr.Is4() {
		return formatErr("ipv4", s, "not 4-octet")
	}
	return nil
}

// validateIPv6 accepts an RFC 4291 textual IPv6 address (no zone id).
func validateIPv6(s string) error {
	if strings.Contains(s, "%") {
		return formatErr("ipv6", s, "zone id not allowed")
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return formatErrCause("ipv6", s, err)
	}
	if !addr.Is6() || addr.Is4() {
		return formatErr("ipv6", s, "not 16-octet")
	}
	return nil
}

// validateHostname accepts an RFC 1123 hostname (letter/digit/hyphen labels,
// label ≤ 63, total ≤ 255). Trailing dot is rejected (the test suite treats
// `example.` as invalid, contrary to RFC 1034 zone notation).
func validateHostname(s string) error {
	if s == "" || len(s) > 255 {
		return formatErr("hostname", s, "bad length")
	}
	if strings.HasSuffix(s, ".") {
		return formatErr("hostname", s, "trailing dot")
	}
	for lbl := range strings.SplitSeq(s, ".") {
		if !isHostnameLabel(lbl) {
			return formatErr("hostname", s, "bad label")
		}
		// Reject ACE-style labels with hyphens at positions 3-4 unless the
		// prefix is the IDN ACE marker xn-- (case-insensitive).
		low := strings.ToLower(lbl)
		if !strings.HasPrefix(low, "xn--") && len(lbl) >= 4 &&
			lbl[2] == '-' && lbl[3] == '-' {
			return formatErr("hostname", s, "hyphens in positions 3-4")
		}
	}
	return nil
}

func isHostnameLabel(lbl string) bool {
	if lbl == "" || len(lbl) > 63 {
		return false
	}
	if lbl[0] == '-' || lbl[len(lbl)-1] == '-' {
		return false
	}
	for i := range len(lbl) {
		c := lbl[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') &&
			(c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

// validateIDNHostname accepts a hostname that may contain non-ASCII (RFC
// 5891). Pragmatic stdlib-only implementation: any UTF-8 hostname whose
// labels look reasonable (no NULs, no control chars, length bounds) is
// accepted. Bidi and full UTS#46 normalization are not implemented; that is
// a documented limitation (see Phase 6 closeout in the requirements doc).
func validateIDNHostname(s string) error {
	if s == "" || len(s) > 255*4 {
		return formatErr("idn-hostname", s, "bad length")
	}
	for _, r := range s {
		if r == 0 || r < 0x20 || r == 0x7F {
			return formatErr("idn-hostname", s, "control character")
		}
		if r >= 0xD800 && r <= 0xDFFF {
			return formatErr("idn-hostname", s, "surrogate")
		}
		if !isIDNAllowedRune(r) {
			return formatErr("idn-hostname", s, fmt.Sprintf("disallowed rune U+%04X", r))
		}
	}
	s = strings.TrimSuffix(s, ".")
	if s == "" {
		return formatErr("idn-hostname", s, "empty after trailing dot")
	}
	for lbl := range strings.SplitSeq(s, ".") {
		if lbl == "" {
			return formatErr("idn-hostname", s, "empty label")
		}
		if utf8.RuneCountInString(lbl) > 63 {
			return formatErr("idn-hostname", s, "label too long")
		}
		runes := []rune(lbl)
		if runes[0] == '-' || runes[len(runes)-1] == '-' {
			return formatErr("idn-hostname", s, "hyphen at label boundary")
		}
		if unicode.Is(unicode.M, runes[0]) {
			return formatErr("idn-hostname", s, "leading combining mark")
		}
	}
	return nil
}

// isIDNAllowedRune is a coarse filter rejecting characters that are
// unambiguously not part of a hostname.
func isIDNAllowedRune(r rune) bool {
	if r == '-' || r == '.' {
		return true
	}
	if r >= '0' && r <= '9' {
		return true
	}
	if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
		return true
	}
	if r < 0x80 {
		return false
	}
	if unicode.Is(unicode.C, r) || unicode.Is(unicode.Z, r) {
		return false
	}
	return true
}

// validateEmail accepts an addr-spec (RFC 5321/5322). The local part may be
// dot-atom or quoted-string; the domain may be a hostname or an
// address-literal in brackets ([IPv4] / [IPv6:...]).
func validateEmail(s string) error {
	if !isASCII(s) {
		return formatErr("email", s, "non-ASCII; use idn-email")
	}
	at, err := splitAddrSpec(s)
	if err != nil {
		return formatErrCause("email", s, err)
	}
	if err := validateAddrLocal(s[:at], false); err != nil {
		return formatErrCause("email", s, err)
	}
	domain := s[at+1:]
	if err := validateAddrDomain(domain, false); err != nil {
		return formatErrCause("email", s, err)
	}
	return nil
}

// validateIDNEmail accepts an internationalized email address (RFC 6531).
// The local part may contain UTF-8; the domain may be an IDN hostname or an
// address-literal.
func validateIDNEmail(s string) error {
	if containsCtrl(s) {
		return formatErr("idn-email", s, "control character")
	}
	at, err := splitAddrSpec(s)
	if err != nil {
		return formatErrCause("idn-email", s, err)
	}
	if err := validateAddrLocal(s[:at], true); err != nil {
		return formatErrCause("idn-email", s, err)
	}
	domain := s[at+1:]
	if err := validateAddrDomain(domain, true); err != nil {
		return formatErrCause("idn-email", s, err)
	}
	return nil
}

// errBadAddr is the sentinel for malformed email-spec components.
var errBadAddr = errors.New("bad addr-spec")

// splitAddrSpec finds the position of the addr-spec separator '@', honoring
// RFC 5321 quoted-string local parts. Returns the index of '@' on success.
func splitAddrSpec(s string) (int, error) {
	if s == "" {
		return -1, fmt.Errorf("%w: empty", errBadAddr)
	}
	i := 0
	if s[0] == '"' {
		i = 1
		for i < len(s) {
			c := s[i]
			if c == '\\' {
				if i+1 >= len(s) {
					return -1, fmt.Errorf("%w: trailing backslash", errBadAddr)
				}
				i += 2
				continue
			}
			if c == '"' {
				i++
				break
			}
			i++
		}
	}
	for ; i < len(s); i++ {
		if s[i] == '@' {
			if i == 0 {
				return -1, fmt.Errorf("%w: empty local part", errBadAddr)
			}
			if i == len(s)-1 {
				return -1, fmt.Errorf("%w: empty domain", errBadAddr)
			}
			return i, nil
		}
	}
	return -1, fmt.Errorf("%w: missing @", errBadAddr)
}

// validateAddrLocal validates the local part: dot-atom-text or quoted-string.
// allowUnicode lets non-ASCII bytes through (RFC 6531 idn-email).
func validateAddrLocal(local string, allowUnicode bool) error {
	if local == "" {
		return fmt.Errorf("%w: empty local part", errBadAddr)
	}
	if local[0] == '"' {
		return validateQuotedLocal(local, allowUnicode)
	}
	return validateDotAtomLocal(local, allowUnicode)
}

// atext per RFC 5322: ALPHA / DIGIT / "!#$%&'*+-/=?^_`{|}~".
func isATextByte(c byte, allowUnicode bool) bool {
	if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
		return true
	}
	if strings.IndexByte("!#$%&'*+-/=?^_`{|}~", c) >= 0 {
		return true
	}
	if allowUnicode && c >= 0x80 {
		return true
	}
	return false
}

func validateDotAtomLocal(local string, allowUnicode bool) error {
	if strings.HasPrefix(local, ".") || strings.HasSuffix(local, ".") {
		return fmt.Errorf("%w: local starts/ends with dot", errBadAddr)
	}
	if strings.Contains(local, "..") {
		return fmt.Errorf("%w: consecutive dots", errBadAddr)
	}
	for i := range len(local) {
		c := local[i]
		if c == '.' {
			continue
		}
		if !isATextByte(c, allowUnicode) {
			return fmt.Errorf("%w: invalid char %q in local", errBadAddr, c)
		}
	}
	return nil
}

func validateQuotedLocal(local string, allowUnicode bool) error {
	if len(local) < 2 || local[0] != '"' || local[len(local)-1] != '"' {
		return fmt.Errorf("%w: malformed quoted local", errBadAddr)
	}
	inner := local[1 : len(local)-1]
	i := 0
	for i < len(inner) {
		c := inner[i]
		switch {
		case c == '\\':
			if i+1 >= len(inner) {
				return fmt.Errorf("%w: dangling backslash", errBadAddr)
			}
			i += 2
			continue
		case c == '"':
			return fmt.Errorf("%w: unescaped quote", errBadAddr)
		case c < 0x20 || c == 0x7F:
			return fmt.Errorf("%w: control character", errBadAddr)
		case c >= 0x80 && !allowUnicode:
			return fmt.Errorf("%w: non-ASCII in quoted local", errBadAddr)
		}
		i++
	}
	return nil
}

// validateAddrDomain validates the domain part: hostname, IDN hostname, or
// bracketed address-literal.
func validateAddrDomain(domain string, allowIDN bool) error {
	if domain == "" {
		return fmt.Errorf("%w: empty domain", errBadAddr)
	}
	if domain[0] == '[' {
		if domain[len(domain)-1] != ']' {
			return fmt.Errorf("%w: unmatched [", errBadAddr)
		}
		inner := domain[1 : len(domain)-1]
		if strings.HasPrefix(inner, "IPv6:") {
			return validateIPv6(inner[len("IPv6:"):])
		}
		return validateIPv4(inner)
	}
	if isASCII(domain) {
		return validateHostname(domain)
	}
	if !allowIDN {
		return fmt.Errorf("%w: non-ASCII domain in ASCII-only email", errBadAddr)
	}
	return validateIDNHostname(domain)
}

// validateRegex parses s as ECMA-262 regex syntax. Go's regexp/syntax in Perl
// mode is a close-enough superset; some ECMA constructs (lookbehind etc.) are
// not supported and surface as errors here.
func validateRegex(s string) error {
	if _, err := syntax.Parse(s, syntax.Perl); err != nil {
		return formatErrCause("regex", s, err)
	}
	return nil
}
