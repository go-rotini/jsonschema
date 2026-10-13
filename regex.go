package jsonschema

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	// errECMAUnsupported marks valid ECMA-262 syntax that RE2 cannot run:
	// backreferences and lookaround.
	errECMAUnsupported = errors.New("not supported by RE2")
	// errECMASyntax marks a pattern that is not valid ECMA-262 syntax.
	errECMASyntax = errors.New("invalid ECMA-262 regex")
)

// ECMA-262 white space and line terminators, the set \s matches, written
// as class members.
const ecmaSpace = `\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

// compileECMA compiles an ECMA-262 pattern (with the "u" flag's escape
// rules, as JSON Schema recommends) into an RE2 regexp with the same
// meaning. Backreferences and lookaround have no RE2 equivalent and are
// reported as errors.
func compileECMA(p string) (*regexp.Regexp, error) {
	re2, err := translateECMA(p)
	if err != nil {
		return nil, err
	}
	re, err := regexp.Compile(re2)
	if err != nil {
		return nil, fmt.Errorf("compile pattern: %w", err)
	}
	return re, nil
}

// translateECMA rewrites ECMA-262 regex source as RE2 syntax: \uXXXX,
// \u{...}, \xHH, \cX and \0 escapes become \x{...}; \s, \S and . take
// their ECMA meanings; [] and [^] match nothing and anything. The error
// wraps errECMASyntax for invalid ECMA syntax and errECMAUnsupported for
// valid syntax RE2 cannot express.
func translateECMA(p string) (string, error) {
	t, err := translateECMAAll(p)
	if err != nil {
		return "", err
	}
	if t.unsupported != nil {
		return "", t.unsupported
	}
	return t.out.String(), nil
}

// translateECMAAll is translateECMA that reads the whole pattern even past
// constructs RE2 cannot run. It reports the first of those as unsupported
// and stands an empty group in for each (a lookaround keeps its contents
// as a plain group), so that the rest of the pattern can still be
// syntax-checked.
// The translation is t.out; t.unsupported holds the first unsupported
// construct.
func translateECMAAll(p string) (*ecmaTranslator, error) {
	t := &ecmaTranslator{p: p}
	t.out.Grow(len(p) + 8)
	if err := t.run(); err != nil {
		return nil, err
	}
	return t, nil
}

// ecmaTranslator holds the state of one translateECMAAll call.
type ecmaTranslator struct {
	p           string
	out         strings.Builder
	unsupported error
	// inClass is set inside a character class, which started at classStart
	// in out (just after its "[" or "[^"); negated is set for "[^".
	inClass    bool
	negated    bool
	classStart int
	// classNonSpace records a \S in the current class.
	classNonSpace bool
}

// unsupportedConstruct records an unsupported construct and writes a
// placeholder for it.
func (t *ecmaTranslator) unsupportedConstruct(what, placeholder string) {
	if t.unsupported == nil {
		t.unsupported = fmt.Errorf("%w: %s", errECMAUnsupported, what)
	}
	t.out.WriteString(placeholder)
}

func (t *ecmaTranslator) run() error {
	p := t.p
	for i := 0; i < len(p); {
		c := p[i]
		switch {
		case c == '\\':
			n, err := t.escape(i)
			if err != nil {
				return err
			}
			i += n
			continue
		case t.inClass && c == ']':
			t.inClass = false
			t.closeClass()
			i++
			continue
		case t.inClass && c == '[':
			t.out.WriteString(`\[`)
			i++
			continue
		case !t.inClass && c == '[':
			switch {
			case strings.HasPrefix(p[i:], "[]"):
				t.out.WriteString(`[^\x00-\x{10FFFF}]`)
				i += 2
				continue
			case strings.HasPrefix(p[i:], "[^]"):
				t.out.WriteString(`[\x00-\x{10FFFF}]`)
				i += 3
				continue
			}
			t.inClass, t.negated, t.classNonSpace = true, false, false
			t.out.WriteByte('[')
			i++
			if i < len(p) && p[i] == '^' {
				t.out.WriteByte('^')
				t.negated = true
				i++
			}
			t.classStart = t.out.Len()
			// A leading ] is a literal in RE2 but closes an empty class in
			// ECMA; [] and [^] were handled above.
			continue
		case !t.inClass && c == '.':
			t.out.WriteString(`[^\n\r\x{2028}\x{2029}]`)
			i++
			continue
		case !t.inClass && c == '(' && strings.HasPrefix(p[i:], "(?"):
			n, err := t.group(p[i:])
			if err != nil {
				return err
			}
			i += n
			continue
		}
		t.out.WriteByte(c)
		i++
	}
	if t.inClass {
		return fmt.Errorf("%w: unterminated character class", errECMASyntax)
	}
	return nil
}

// closeClass ends the character class just written. A class holding \S
// has no RE2 class form, since RE2's \S is ASCII-only: [\S...] becomes an
// alternation with the negated ECMA space set, and [^\S...] the ECMA
// spaces the rest of the class does not match.
func (t *ecmaTranslator) closeClass() {
	if !t.classNonSpace {
		t.out.WriteByte(']')
		return
	}
	full := t.out.String()
	open := t.classStart - 1
	if t.negated {
		open--
	}
	rest := full[t.classStart:]
	t.out.Reset()
	t.out.WriteString(full[:open])
	switch {
	case t.negated:
		t.out.WriteString(spacesOutside(rest))
	case rest == "":
		t.out.WriteString("[^" + ecmaSpace + "]")
	default:
		t.out.WriteString("(?:[" + rest + "]|[^" + ecmaSpace + "])")
	}
}

// ecmaSpaceRunes lists the characters of ecmaSpace.
var ecmaSpaceRunes = []rune{'\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680,
	0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005, 0x2006, 0x2007, 0x2008, 0x2009, 0x200a,
	0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff}

// spacesOutside returns an RE2 class matching the ECMA spaces that the
// class body members (already in RE2 syntax) does not match.
func spacesOutside(members string) string {
	var in *regexp.Regexp
	if members != "" {
		var err error
		if in, err = regexp.Compile("[" + members + "]"); err != nil {
			// An invalid class stays invalid, for the caller to report.
			return "[" + members + "]"
		}
	}
	var b strings.Builder
	for _, r := range ecmaSpaceRunes {
		if in == nil || !in.MatchString(string(r)) {
			fmt.Fprintf(&b, `\x{%x}`, r)
		}
	}
	if b.Len() == 0 {
		return `[^\x00-\x{10FFFF}]`
	}
	return "[" + b.String() + "]"
}

// group handles a group opening "(?" at the start of s and returns the
// number of bytes consumed.
func (t *ecmaTranslator) group(s string) (int, error) {
	switch {
	case strings.HasPrefix(s, "(?:"):
		t.out.WriteString("(?:")
		return 3, nil
	case strings.HasPrefix(s, "(?="), strings.HasPrefix(s, "(?!"):
		t.unsupportedConstruct("lookaround", "(?:")
		return 3, nil
	case strings.HasPrefix(s, "(?<="), strings.HasPrefix(s, "(?<!"):
		t.unsupportedConstruct("lookaround", "(?:")
		return 4, nil
	case strings.HasPrefix(s, "(?<"):
		end := strings.IndexByte(s, '>')
		if end < 0 || !isGroupName(s[3:end]) {
			return 0, fmt.Errorf("%w: bad group name", errECMASyntax)
		}
		t.out.WriteString("(?P<")
		t.out.WriteString(s[3:end])
		t.out.WriteByte('>')
		return end + 1, nil
	}
	return 0, fmt.Errorf("%w: %q is not an ECMA-262 group", errECMASyntax, s[:min(len(s), 4)])
}

func isGroupName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r != '_' && r != '$' && !isASCIILetter(r) && (i == 0 || r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func isASCIILetter(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }

// escape translates the escape starting at p[i] (a backslash) and returns
// the number of bytes consumed.
func (t *ecmaTranslator) escape(i int) (int, error) {
	p, out, inClass := t.p, &t.out, t.inClass
	if i+1 >= len(p) {
		return 0, fmt.Errorf("%w: trailing backslash", errECMASyntax)
	}
	c := p[i+1]
	switch c {
	case 'd', 'D', 'w', 'W', 'f', 'n', 'r', 't', 'v':
		out.WriteString(p[i : i+2])
		return 2, nil
	case 'S':
		if inClass {
			t.classNonSpace = true
			return 2, nil
		}
		out.WriteString("[^" + ecmaSpace + "]")
		return 2, nil
	case 'b', 'B', 's':
		return 2, translateClassEscape(out, c, inClass)
	case 'c', '0', 'x', 'u':
		return translateCodeEscape(out, p, i)
	case '1', '2', '3', '4', '5', '6', '7', '8', '9':
		n := 2
		for i+n < len(p) && isDigit(p[i+n]) {
			n++
		}
		t.unsupportedConstruct("backreference", "(?:)")
		return n, nil
	case 'k':
		if i+2 < len(p) && p[i+2] == '<' {
			end := strings.IndexByte(p[i:], '>')
			if end < 0 || !isGroupName(p[i+3:i+end]) {
				return 0, fmt.Errorf("%w: bad group name", errECMASyntax)
			}
			t.unsupportedConstruct("backreference", "(?:)")
			return end + 1, nil
		}
		return 0, fmt.Errorf(`%w: \k`, errECMASyntax)
	case 'p', 'P':
		end := strings.IndexByte(p[i:], '}')
		if i+2 >= len(p) || p[i+2] != '{' || end < 0 {
			return 0, fmt.Errorf(`%w: \%c needs {name}`, errECMASyntax, c)
		}
		name := p[i+3 : i+end]
		if _, v, ok := strings.Cut(name, "="); ok {
			name = v
		}
		fmt.Fprintf(out, `\%c{%s}`, c, name)
		return end + 1, nil
	}
	if strings.IndexByte(`^$\.*+?()[]{}|/`, c) >= 0 || (inClass && c == '-') {
		out.WriteString(p[i : i+2])
		return 2, nil
	}
	r, _ := utf8.DecodeRuneInString(p[i+1:])
	return 0, fmt.Errorf("%w: \\%c is not an ECMA-262 escape", errECMASyntax, r)
}

// translateClassEscape translates \b, \B or \s, whose meaning depends on
// whether they appear inside a character class.
func translateClassEscape(out *strings.Builder, c byte, inClass bool) error {
	switch {
	case c == 'b' && inClass:
		out.WriteString(`\x08`)
	case c == 'B' && inClass:
		return fmt.Errorf(`%w: \B in a class`, errECMASyntax)
	case c == 's' && inClass:
		out.WriteString(ecmaSpace)
	case c == 's':
		out.WriteString("[" + ecmaSpace + "]")
	default:
		out.WriteByte('\\')
		out.WriteByte(c)
	}
	return nil
}

// translateCodeEscape translates the character escapes \cX, \0, \xHH and
// \u... at p[i].
func translateCodeEscape(out *strings.Builder, p string, i int) (int, error) {
	switch p[i+1] {
	case 'c':
		if i+2 < len(p) && isASCIILetter(rune(p[i+2])) {
			fmt.Fprintf(out, `\x{%x}`, p[i+2]%32)
			return 3, nil
		}
		return 0, fmt.Errorf(`%w: \c must be followed by a letter`, errECMASyntax)
	case '0':
		if i+2 < len(p) && isDigit(p[i+2]) {
			return 0, fmt.Errorf("%w: octal escape", errECMASyntax)
		}
		out.WriteString(`\x00`)
		return 2, nil
	case 'x':
		if i+3 < len(p) && isHexString(p[i+2:i+4]) {
			fmt.Fprintf(out, `\x{%s}`, p[i+2:i+4])
			return 4, nil
		}
		return 0, fmt.Errorf(`%w: \x needs two hex digits`, errECMASyntax)
	}
	return translateUnicodeEscape(out, p, i)
}

// translateUnicodeEscape handles \uXXXX (combining a surrogate pair) and
// \u{X...} at p[i].
func translateUnicodeEscape(out *strings.Builder, p string, i int) (int, error) {
	rest := p[i+2:]
	if strings.HasPrefix(rest, "{") {
		end := strings.IndexByte(rest, '}')
		if end < 2 || !isHexString(rest[1:end]) {
			return 0, fmt.Errorf(`%w: bad \u{...} escape`, errECMASyntax)
		}
		v, err := strconv.ParseUint(rest[1:end], 16, 32)
		if err != nil || v > utf8.MaxRune {
			return 0, fmt.Errorf(`%w: \u{...} out of range`, errECMASyntax)
		}
		fmt.Fprintf(out, `\x{%x}`, v)
		return 2 + end + 1, nil
	}
	if len(rest) < 4 || !isHexString(rest[:4]) {
		return 0, fmt.Errorf(`%w: \u needs four hex digits`, errECMASyntax)
	}
	hi := hexValue(rest[:4])
	if hi >= 0xD800 && hi < 0xDC00 && len(rest) >= 10 && rest[4:6] == `\u` && isHexString(rest[6:10]) {
		lo := hexValue(rest[6:10])
		if lo >= 0xDC00 && lo < 0xE000 {
			fmt.Fprintf(out, `\x{%x}`, (hi-0xD800)<<10+(lo-0xDC00)+0x10000)
			return 12, nil
		}
	}
	fmt.Fprintf(out, `\x{%x}`, hi)
	return 6, nil
}

func isHexString(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// hexValue returns the value of s, a short string of hex digits.
func hexValue(s string) uint64 {
	var v uint64
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= 'a':
			c -= 'a' - 10
		case c >= 'A':
			c -= 'A' - 10
		default:
			c -= '0'
		}
		v = v<<4 | uint64(c)
	}
	return v
}
