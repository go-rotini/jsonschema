package jsonschema

import (
	"errors"
	"math"
	"strings"
	"unicode/utf8"
)

// Punycode (RFC 3492) parameters.
const (
	pcBase        = 36
	pcTMin        = 1
	pcTMax        = 26
	pcSkew        = 38
	pcDamp        = 700
	pcInitialBias = 72
	pcInitialN    = 128
)

var errPunycode = errors.New("invalid punycode")

// decodePunycode decodes s, the part of an A-label after "xn--", into the
// Unicode label it encodes.
func decodePunycode(s string) (string, error) {
	var out []rune
	rest := s
	if b := strings.LastIndexByte(s, '-'); b >= 0 {
		for i := range b {
			if s[i] >= utf8.RuneSelf {
				return "", errPunycode
			}
			out = append(out, rune(s[i]))
		}
		rest = s[b+1:]
	}
	n, bias, i := pcInitialN, pcInitialBias, 0
	for pos := 0; pos < len(rest); {
		oldi, w := i, 1
		for k := pcBase; ; k += pcBase {
			if pos >= len(rest) {
				return "", errPunycode
			}
			digit, ok := punycodeDigit(rest[pos])
			pos++
			if !ok || digit > (math.MaxInt32-i)/w {
				return "", errPunycode
			}
			i += digit * w
			t := punycodeThreshold(k, bias)
			if digit < t {
				break
			}
			if w > math.MaxInt32/(pcBase-t) {
				return "", errPunycode
			}
			w *= pcBase - t
		}
		count := len(out) + 1
		bias = punycodeAdapt(i-oldi, count, oldi == 0)
		if i/count > math.MaxInt32-n {
			return "", errPunycode
		}
		n += i / count
		i %= count
		if n > utf8.MaxRune || (n >= 0xD800 && n <= 0xDFFF) {
			return "", errPunycode
		}
		out = append(out, 0)
		copy(out[i+1:], out[i:])
		out[i] = rune(n)
		i++
	}
	return string(out), nil
}

// encodePunycode encodes label, the inverse of [decodePunycode].
func encodePunycode(label string) string {
	runes := []rune(label)
	var out strings.Builder
	for _, r := range runes {
		if r < utf8.RuneSelf {
			out.WriteRune(r)
		}
	}
	basic := out.Len()
	handled := basic
	if basic > 0 {
		out.WriteByte('-')
	}
	n, delta, bias := pcInitialN, 0, pcInitialBias
	for handled < len(runes) {
		m := math.MaxInt32
		for _, r := range runes {
			if int(r) >= n && int(r) < m {
				m = int(r)
			}
		}
		delta += (m - n) * (handled + 1)
		n = m
		for _, r := range runes {
			if int(r) < n {
				delta++
			}
			if int(r) != n {
				continue
			}
			q := delta
			for k := pcBase; ; k += pcBase {
				t := punycodeThreshold(k, bias)
				if q < t {
					break
				}
				out.WriteByte(punycodeChar(t + (q-t)%(pcBase-t)))
				q = (q - t) / (pcBase - t)
			}
			out.WriteByte(punycodeChar(q))
			bias = punycodeAdapt(delta, handled+1, handled == basic)
			delta = 0
			handled++
		}
		delta++
		n++
	}
	return out.String()
}

func punycodeThreshold(k, bias int) int {
	switch {
	case k <= bias:
		return pcTMin
	case k >= bias+pcTMax:
		return pcTMax
	}
	return k - bias
}

func punycodeAdapt(delta, count int, first bool) int {
	if first {
		delta /= pcDamp
	} else {
		delta /= 2
	}
	delta += delta / count
	k := 0
	for delta > ((pcBase-pcTMin)*pcTMax)/2 {
		delta /= pcBase - pcTMin
		k += pcBase
	}
	return k + (pcBase-pcTMin+1)*delta/(delta+pcSkew)
}

func punycodeDigit(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c-'0') + 26, true
	case c >= 'a' && c <= 'z':
		return int(c - 'a'), true
	case c >= 'A' && c <= 'Z':
		return int(c - 'A'), true
	}
	return 0, false
}

func punycodeChar(d int) byte {
	if d < 26 {
		return byte('a' + d)
	}
	return byte('0' + d - 26)
}
