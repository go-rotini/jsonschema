package jsonschema

import (
	"encoding/json"
	"math"
	"math/big"
	"math/bits"
	"slices"
	"strconv"
	"strings"
)

// maxDecimalExp bounds the exponents a [decimal] stores as an int64. A
// number whose exponent is beyond it is kept as "huge", with the exponent
// as decimal text: it is still compared exactly, but never materialized.
const maxDecimalExp = 1e15

// decimal is an exact, unexpanded form of a JSON number: digits × 10^exp.
// Comparing two decimals costs time linear in their digits, however large
// their exponents are.
type decimal struct {
	nan bool
	inf bool
	neg bool
	// digits holds the significant digits, with no leading or trailing
	// zeros; "" means zero.
	digits string
	exp    int64
	// huge is +1 or -1 when the exponent is beyond ±maxDecimalExp; exp is
	// then unused and hugeExp holds the exponent's magnitude as decimal
	// digits, its sign being huge's.
	huge    int8
	hugeExp string
}

// parseNumber converts a JSON number value into a decimal. Values that are
// not numbers, and number text that does not parse, report false.
func parseNumber(v any) (decimal, bool) {
	switch n := v.(type) {
	case json.Number:
		return parseDecimal(string(n))
	case float64:
		switch {
		case math.IsNaN(n):
			return decimal{nan: true}, true
		case math.IsInf(n, 0):
			return decimal{inf: true, neg: n < 0}, true
		}
		return parseDecimal(strconv.FormatFloat(n, 'e', -1, 64))
	case float32:
		return parseNumber(float64(n))
	case int:
		return parseDecimal(strconv.Itoa(n))
	case int8:
		return parseDecimal(strconv.FormatInt(int64(n), 10))
	case int16:
		return parseDecimal(strconv.FormatInt(int64(n), 10))
	case int32:
		return parseDecimal(strconv.FormatInt(int64(n), 10))
	case int64:
		return parseDecimal(strconv.FormatInt(n, 10))
	case uint:
		return parseDecimal(strconv.FormatUint(uint64(n), 10))
	case uint8:
		return parseDecimal(strconv.FormatUint(uint64(n), 10))
	case uint16:
		return parseDecimal(strconv.FormatUint(uint64(n), 10))
	case uint32:
		return parseDecimal(strconv.FormatUint(uint64(n), 10))
	case uint64:
		return parseDecimal(strconv.FormatUint(n, 10))
	case *big.Int:
		if n == nil {
			return decimal{}, false
		}
		return parseDecimal(n.String())
	}
	return decimal{}, false
}

// parseDecimal parses number text: an optional sign, digits with an
// optional fraction, and an optional exponent. "Inf", "Infinity" and "NaN"
// (any case, Inf with an optional sign) parse to their special values, as
// the YAML and TOML adapters produce them.
func parseDecimal(s string) (decimal, bool) {
	var d decimal
	body := s
	if body != "" && (body[0] == '+' || body[0] == '-') {
		d.neg = body[0] == '-'
		body = body[1:]
	}
	switch strings.ToLower(body) {
	case "inf", "infinity":
		d.inf = true
		return d, true
	case "nan":
		if body != s {
			return decimal{}, false
		}
		return decimal{nan: true}, true
	}
	intPart, fracPart, rest := scanMantissa(body)
	if intPart == "" && fracPart == "" {
		return decimal{}, false
	}
	expNeg, expText, ok := scanExponent(rest)
	if !ok {
		return decimal{}, false
	}

	digits := strings.TrimLeft(intPart+fracPart, "0")
	trimmed := strings.TrimRight(digits, "0")
	if trimmed == "" {
		return decimal{}, true // zero, whatever its sign or exponent
	}
	d.digits = trimmed
	shift := int64(len(digits)-len(trimmed)) - int64(len(fracPart))
	expNeg, mag := addSmall(expNeg, expText, shift)
	if e, ok := smallExp(expNeg, mag); ok {
		d.exp = e
		return d, true
	}
	d.huge = 1
	if expNeg {
		d.huge = -1
	}
	d.hugeExp = mag
	return d, true
}

// smallExp returns the exponent of sign neg and magnitude mag (decimal
// digits) when it is within ±maxDecimalExp.
func smallExp(neg bool, mag string) (int64, bool) {
	if len(mag) > 16 {
		return 0, false
	}
	e := int64(digitsValue(mag))
	if e > maxDecimalExp {
		return 0, false
	}
	if neg {
		e = -e
	}
	return e, true
}

// addSmall returns (-1)^neg × mag + delta as a sign and magnitude, where
// mag is decimal digits with no leading zeros ("" for zero) and delta is
// small. It costs time linear in mag's length.
func addSmall(neg bool, mag string, delta int64) (bool, string) {
	if len(mag) <= 17 {
		v := int64(digitsValue(mag))
		if neg {
			v = -v
		}
		v += delta
		if v < 0 {
			return true, strconv.FormatInt(-v, 10)
		}
		if v == 0 {
			return false, ""
		}
		return false, strconv.FormatInt(v, 10)
	}
	// mag is at least 10^17, so the sign does not change: the magnitude
	// grows by |delta| when delta has mag's sign and shrinks otherwise.
	d := delta
	if neg {
		d = -d
	}
	if d >= 0 {
		return neg, addDigits(mag, uint64(d))
	}
	return neg, subDigits(mag, uint64(-d))
}

// digitsValue returns the value of s, at most 19 decimal digits.
func digitsValue(s string) uint64 {
	var v uint64
	for i := range len(s) {
		v = v*10 + uint64(s[i]-'0')
	}
	return v
}

// addDigits returns the decimal digits of mag + n.
func addDigits(mag string, n uint64) string {
	b := []byte(mag)
	for i := len(b) - 1; i >= 0 && n > 0; i-- {
		v := uint64(b[i]-'0') + n%10
		n /= 10
		if v >= 10 {
			v -= 10
			n++
		}
		b[i] = byte('0' + v)
	}
	if n > 0 {
		return strconv.FormatUint(n, 10) + string(b)
	}
	return string(b)
}

// subDigits returns the decimal digits of mag - n, for n < mag.
func subDigits(mag string, n uint64) string {
	b := []byte(mag)
	for i := len(b) - 1; i >= 0 && n > 0; i-- {
		v := int(b[i]-'0') - int(n%10)
		n /= 10
		if v < 0 {
			v += 10
			n++
		}
		b[i] = byte('0' + v)
	}
	return strings.TrimLeft(string(b), "0")
}

// order returns the sign and magnitude of the position of d's leading
// digit, its exponent plus its digit count: |d| < 10^order, and |d| ≥
// 10^(order-1).
func (d decimal) order() (bool, string) {
	if d.huge == 0 {
		o := d.exp + int64(len(d.digits))
		if o < 0 {
			return true, strconv.FormatInt(-o, 10)
		}
		return false, strconv.FormatInt(o, 10)
	}
	return addSmall(d.huge < 0, d.hugeExp, int64(len(d.digits)))
}

// cmpSigned compares two signed decimal integers given as signs and
// magnitudes without leading zeros.
func cmpSigned(aNeg bool, a string, bNeg bool, b string) int {
	if a == "" {
		aNeg = false
	}
	if b == "" {
		bNeg = false
	}
	if aNeg != bNeg {
		if aNeg {
			return -1
		}
		return 1
	}
	c := compareInts(len(a), len(b))
	if c == 0 {
		c = strings.Compare(a, b)
	}
	if aNeg {
		return -c
	}
	return c
}

// scanMantissa splits the digits before and after the decimal point off
// the front of s and returns the rest.
func scanMantissa(s string) (intPart, fracPart, rest string) {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	intPart, rest = s[:i], s[i:]
	if rest != "" && rest[0] == '.' {
		j := 1
		for j < len(rest) && isDigit(rest[j]) {
			j++
		}
		fracPart, rest = rest[1:j], rest[j:]
	}
	return intPart, fracPart, rest
}

// scanExponent parses s as an optional exponent ("e", an optional sign and
// digits), which must be all that is left of the number. expText is the
// exponent's digits without leading zeros.
func scanExponent(s string) (expNeg bool, expText string, ok bool) {
	if s == "" {
		return false, "", true
	}
	if s[0] != 'e' && s[0] != 'E' {
		return false, "", false
	}
	s = s[1:]
	if s != "" && (s[0] == '+' || s[0] == '-') {
		expNeg = s[0] == '-'
		s = s[1:]
	}
	if s == "" {
		return false, "", false
	}
	for i := range len(s) {
		if !isDigit(s[i]) {
			return false, "", false
		}
	}
	return expNeg, strings.TrimLeft(s, "0"), true
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func (d decimal) isZero() bool { return !d.nan && !d.inf && d.digits == "" }

func (d decimal) sign() int {
	switch {
	case d.isZero():
		return 0
	case d.neg:
		return -1
	}
	return 1
}

// isInteger reports whether d has no fractional part.
func (d decimal) isInteger() bool {
	switch {
	case d.nan || d.inf:
		return false
	case d.isZero():
		return true
	case d.huge != 0:
		return d.huge > 0
	}
	return d.exp >= 0
}

// cmp compares a and b, reporting false when they cannot be ordered: either
// is NaN.
func (d decimal) cmp(b decimal) (int, bool) {
	a := d
	if a.nan || b.nan {
		return 0, false
	}
	if a.inf || b.inf {
		ia, ib := infRank(a), infRank(b)
		switch {
		case ia < ib:
			return -1, true
		case ia > ib:
			return 1, true
		case ia != 0:
			return 0, true
		}
	}
	sa, sb := a.sign(), b.sign()
	if sa != sb {
		return compareInts(sa, sb), true
	}
	if sa == 0 {
		return 0, true
	}
	return sa * cmpMagnitude(a, b), true
}

// infRank is -1 for -Inf, +1 for +Inf and 0 for a finite value.
func infRank(d decimal) int {
	switch {
	case !d.inf:
		return 0
	case d.neg:
		return -1
	}
	return 1
}

// cmpMagnitude compares |a| and |b| for non-zero finite a and b: first by
// the position of the leading digit, then by the digits.
func cmpMagnitude(a, b decimal) int {
	if a.huge == 0 && b.huge == 0 {
		oa := a.exp + int64(len(a.digits))
		ob := b.exp + int64(len(b.digits))
		if oa != ob {
			if oa < ob {
				return -1
			}
			return 1
		}
	} else {
		aNeg, ao := a.order()
		bNeg, bo := b.order()
		if c := cmpSigned(aNeg, ao, bNeg, bo); c != 0 {
			return c
		}
	}
	return strings.Compare(a.digits, b.digits)
}

func compareInts(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// equal reports whether a and b are the same number.
func (d decimal) equal(b decimal) bool {
	c, ok := d.cmp(b)
	return ok && c == 0
}

// key writes a canonical form of a non-NaN d to sb: two decimals that are
// equal write the same text.
func (d decimal) key(sb *strings.Builder) {
	switch {
	case d.inf:
		if d.neg {
			sb.WriteString("-inf")
		} else {
			sb.WriteString("+inf")
		}
		return
	case d.isZero():
		sb.WriteByte('0')
		return
	}
	if d.neg {
		sb.WriteByte('-')
	}
	sb.WriteString(d.digits)
	sb.WriteByte('e')
	if d.huge != 0 {
		sb.WriteString(strconv.Itoa(int(d.huge)))
		sb.WriteByte('h')
		sb.WriteString(d.hugeExp)
		return
	}
	sb.WriteString(strconv.FormatInt(d.exp, 10))
}

// divisor is a multipleOf value prepared for repeated division tests: its
// digits b split as 2^p × 5^q × m with m coprime to 10, and its exponent.
type divisor struct {
	d    decimal
	m    *big.Int
	p, q int64
}

func newDivisor(d decimal) divisor {
	b, _ := new(big.Int).SetString(d.digits, 10)
	p := int64(b.TrailingZeroBits())
	b.Rsh(b, uint(p))
	q := stripFives(b)
	return divisor{d: d, m: b, p: p, q: q}
}

// stripFives divides every factor of 5 out of n and returns how many there
// were. It divides by 5^(2^i) for falling i, so the number of divisions is
// logarithmic in n's size rather than linear in the count.
func stripFives(n *big.Int) int64 {
	if n.Sign() == 0 {
		return 0
	}
	powers := []*big.Int{big.NewInt(5)}
	for {
		last := powers[len(powers)-1]
		if last.CmpAbs(n) > 0 || len(last.Bits())*2 > len(n.Bits())+1 {
			break
		}
		powers = append(powers, new(big.Int).Mul(last, last))
	}
	var q, r big.Int
	var count int64
	for i, p := range slices.Backward(powers) {
		for {
			q.QuoRem(n, p, &r)
			if r.Sign() != 0 {
				break
			}
			n.Set(&q)
			count += 1 << i
		}
	}
	return count
}

// divides reports whether x is an integer multiple of the divisor. x =
// a × 10^ea and the divisor is b × 10^eb, so x/divisor = (a/b) × 10^k with k
// = ea-eb; that is an integer exactly when b divides a × 10^k. With a and b
// free of trailing zeros, k < 0 never works. For k ≥ 0, a × 10^k supplies k
// factors each of 2 and 5, so a itself must hold m, 2^(p-k) and 5^(q-k):
// one remainder against their product, which is no larger than b.
func (dv divisor) divides(x decimal) (bool, bool) {
	switch {
	case x.nan || x.inf:
		return false, false
	case x.isZero():
		return true, true
	case x.huge < 0:
		return false, true
	}
	need := dv.m
	xe, ok := x.exp, x.huge == 0
	if x.huge > 0 && len(x.hugeExp) <= 18 {
		xe = int64(digitsValue(x.hugeExp))
		ok = true
	}
	// Otherwise x's exponent is beyond 10^18, which leaves k far above
	// p and q.
	if ok {
		k := xe - dv.d.exp
		if k < 0 {
			return false, true
		}
		if e2, e5 := dv.p-k, dv.q-k; e2 > 0 || e5 > 0 {
			need = new(big.Int).Set(dv.m)
			if e2 > 0 {
				need.Lsh(need, uint(e2))
			}
			if e5 > 0 {
				need.Mul(need, new(big.Int).Exp(big.NewInt(5), big.NewInt(e5), nil))
			}
		}
	}
	return digitsDivisible(x.digits, need), true
}

// digitsDivisible reports whether the decimal digits are a multiple of n.
// It reads the digits 18 at a time, carrying the remainder, so the cost is
// linear in the number of digits and never converts them as a whole.
func digitsDivisible(digits string, n *big.Int) bool {
	const chunk = 18
	head := len(digits) % chunk
	if head == 0 {
		head = chunk
	}
	if n.IsUint64() {
		m := n.Uint64()
		var r uint64
		for i, end := 0, head; i < len(digits); i, end = end, end+chunk {
			c := digitsValue(digits[i:end])
			hi, lo := bits.Mul64(r, pow10u64[end-i])
			lo, carry := bits.Add64(lo, c, 0)
			_, r = bits.Div64(hi+carry, lo, m)
		}
		return r == 0
	}
	var r, c, scale big.Int
	for i, end := 0, head; i < len(digits); i, end = end, end+chunk {
		u := digitsValue(digits[i:end])
		scale.SetUint64(pow10u64[end-i])
		r.Mul(&r, &scale)
		r.Add(&r, c.SetUint64(u))
		r.Mod(&r, n)
	}
	return r.Sign() == 0
}

// pow10u64[i] is 10^i for i up to 18.
var pow10u64 = [...]uint64{1, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9,
	1e10, 1e11, 1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18}
