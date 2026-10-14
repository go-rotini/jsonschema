package jsonschema

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"
	"time"
)

func validOf(t *testing.T, schema, instance string) bool {
	t.Helper()
	s, err := Compile([]byte(schema))
	if err != nil {
		t.Fatalf("compile %s: %v", schema, err)
	}
	res, err := s.Validate([]byte(instance))
	if err != nil {
		t.Fatalf("validate %s: %v", instance, err)
	}
	return res.Valid
}

// TestNumericKeywordsHugeExponents checks that numbers whose exponents are
// too large to expand are still judged by value, never skipped.
func TestNumericKeywordsHugeExponents(t *testing.T) {
	cases := []struct {
		schema, instance string
		want             bool
	}{
		{`{"maximum":10}`, `1e9999999`, false},
		{`{"maximum":10}`, `1e99999999999999999999`, false},
		{`{"maximum":10}`, `-1e9999999`, true},
		{`{"maximum":10}`, `1e-9999999`, true},
		{`{"minimum":0}`, `-1e9999999`, false},
		{`{"minimum":0}`, `-1e-99999999999999999999`, false},
		{`{"exclusiveMaximum":0}`, `1e9999999`, false},
		{`{"exclusiveMinimum":0}`, `1e-9999999`, true},
		{`{"multipleOf":3}`, `1e-9999999`, false},
		{`{"multipleOf":3}`, `3e9999999`, true},
		{`{"multipleOf":3}`, `1e9999999`, false},
		{`{"multipleOf":0.5}`, `1e999999`, true},
		{`{"type":"integer"}`, `1e9999999`, true},
		{`{"type":"integer"}`, `1e-9999999`, false},
		{`{"type":"integer"}`, `0e-9999999`, true},
		{`{"const":1e9999999}`, `1e9999999`, true},
		{`{"const":1e9999999}`, `10e9999998`, true},
		{`{"enum":[1e400]}`, `1e401`, false},
	}
	for _, tc := range cases {
		start := time.Now()
		if got := validOf(t, tc.schema, tc.instance); got != tc.want {
			t.Errorf("%s with %s: valid=%v, want %v", tc.schema, tc.instance, got, tc.want)
		}
		if d := time.Since(start); d > 5*time.Second {
			t.Errorf("%s with %s took %v", tc.schema, tc.instance, d)
		}
	}
}

// TestNumberArrayCostIsBounded checks that many huge-exponent numbers
// validate quickly.
func TestNumberArrayCostIsBounded(t *testing.T) {
	inst := "[" + strings.TrimSuffix(strings.Repeat("1e999999,", 200), ",") + "]"
	start := time.Now()
	if validOf(t, `{"items":{"maximum":10,"multipleOf":7,"type":"integer"}}`, inst) {
		t.Fatal("want invalid")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("took %v", d)
	}
}

// TestDecimalMatchesBigRat compares decimal ordering, equality and
// divisibility with big.Rat over random small numbers.
func TestDecimalMatchesBigRat(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	gen := func() string {
		s := strconv.Itoa(r.IntN(2000) - 1000)
		if r.IntN(2) == 0 {
			s += "." + strconv.Itoa(r.IntN(1000))
		}
		if r.IntN(3) == 0 {
			s += "e" + strconv.Itoa(r.IntN(9)-4)
		}
		return s
	}
	for range 20000 {
		as, bs := gen(), gen()
		ra, _ := new(big.Rat).SetString(as)
		rb, _ := new(big.Rat).SetString(bs)
		da, _ := parseDecimal(as)
		db, _ := parseDecimal(bs)
		if c, ok := da.cmp(db); !ok || c != ra.Cmp(rb) {
			t.Fatalf("cmp(%s, %s) = %d,%v; want %d", as, bs, c, ok, ra.Cmp(rb))
		}
		if da.isInteger() != ra.IsInt() {
			t.Fatalf("isInteger(%s) = %v", as, da.isInteger())
		}
		if rb.Sign() > 0 {
			want := new(big.Rat).Quo(ra, rb).IsInt()
			if got, ok := newDivisor(db).divides(da); !ok || got != want {
				t.Fatalf("divides(%s, %s) = %v; want %v", as, bs, got, want)
			}
		}
		var ka, kb strings.Builder
		da.key(&ka)
		db.key(&kb)
		if (ka.String() == kb.String()) != (ra.Cmp(rb) == 0) {
			t.Fatalf("%s and %s have keys %q and %q", as, bs, ka.String(), kb.String())
		}
	}
}

// TestUniqueItemsLinear checks that uniqueItems stays fast on large arrays
// and reports the same pair as a pairwise scan would.
func TestUniqueItemsLinear(t *testing.T) {
	var sb strings.Builder
	sb.WriteByte('[')
	for i := range 10000 {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, `{"a":%d,"b":[1,"x"]}`, i)
	}
	sb.WriteByte(']')
	start := time.Now()
	if !validOf(t, `{"uniqueItems":true}`, sb.String()) {
		t.Fatal("want valid")
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("took %v", d)
	}

	s := MustCompile([]byte(`{"uniqueItems":true}`))
	for _, tc := range []struct{ inst, want string }{
		{`["a","b","b","a"]`, "array items 0 and 3 are not unique"},
		{`[1,{"x":1.0,"y":[2]},3,{"y":[2e0],"x":1}]`, "array items 1 and 3 are not unique"},
		{`[1,2,1.0,2]`, "array items 0 and 2 are not unique"},
	} {
		res, err := s.Validate([]byte(tc.inst))
		if err != nil || res.Valid || res.Errors[0].Message != tc.want {
			t.Errorf("%s: res=%+v err=%v; want %q", tc.inst, res, err, tc.want)
		}
	}
	if !validOf(t, `{"uniqueItems":true}`, `["1",1,true,null,[1],{"1":1}]`) {
		t.Error("distinct types reported equal")
	}
}

// TestCanonicalKeyMatchesCanonicalEqual cross-checks the two equality
// paths on a set of values.
func TestCanonicalKeyMatchesCanonicalEqual(t *testing.T) {
	vals := []any{nil, true, false, "", "a", json.Number("1"), json.Number("1.0"), float64(1),
		int(2), json.Number("2e0"), []any{}, []any{json.Number("1")}, []any{float64(1)},
		map[string]any{}, map[string]any{"a": json.Number("1")}, map[string]any{"a": int(1)},
		map[string]any{"a": "1"}, []any{"a", "b"}, []any{"ab"},
		uint64(1), uint8(2), uint64(math.MaxUint64), json.Number("18446744073709551615"),
		json.Number("abc"), json.Number("abc"), math.NaN(), json.Number("NaN"),
		[]any{math.NaN()}, map[string]any{"a": math.NaN()}, float32(1.5), json.Number("1.5"),
		new(big.Int).Lsh(big.NewInt(1), 70), json.Number("1180591620717411303424")}
	nan := 0
	for _, a := range vals {
		for _, b := range vals {
			var ka, kb strings.Builder
			canonicalKey(&ka, a, &nan)
			canonicalKey(&kb, b, &nan)
			if (ka.String() == kb.String()) != canonicalEqual(a, b) {
				t.Errorf("%#v vs %#v: keys %q %q, canonicalEqual=%v", a, b, ka.String(), kb.String(), canonicalEqual(a, b))
			}
		}
	}
}

// TestDraft4BooleanExclusiveBounds checks the Draft 4 boolean form of
// exclusiveMaximum / exclusiveMinimum.
func TestDraft4BooleanExclusiveBounds(t *testing.T) {
	const d4 = `"$schema":"http://json-schema.org/draft-04/schema#",`
	cases := []struct {
		schema, instance string
		want             bool
	}{
		{`{` + d4 + `"maximum":3,"exclusiveMaximum":true}`, `3`, false},
		{`{` + d4 + `"maximum":3,"exclusiveMaximum":true}`, `2.9`, true},
		{`{` + d4 + `"maximum":3,"exclusiveMaximum":false}`, `3`, true},
		{`{` + d4 + `"minimum":3,"exclusiveMinimum":true}`, `3`, false},
		{`{` + d4 + `"minimum":3}`, `3`, true},
		{`{` + d4 + `"type":"integer"}`, `1.0`, false},
		{`{` + d4 + `"type":"integer"}`, `1`, true},
		{`{"type":"integer"}`, `1.0`, true},
	}
	for _, tc := range cases {
		if got := validOf(t, tc.schema, tc.instance); got != tc.want {
			t.Errorf("%s with %s: valid=%v, want %v", tc.schema, tc.instance, got, tc.want)
		}
	}
}

// TestLargeIntegerBounds checks that integer bounds beyond the int range
// compile and act as "no effective limit".
func TestLargeIntegerBounds(t *testing.T) {
	cases := []struct {
		schema, instance string
		want             bool
	}{
		{`{"maxLength":1e30}`, `"abc"`, true},
		{`{"minLength":1e19}`, `"abc"`, false},
		{`{"maxItems":18446744073709551616}`, `[1,2]`, true},
		{`{"maxProperties":1e400}`, `{"a":1}`, true},
		{`{"maxLength":2.0}`, `"abc"`, false},
	}
	for _, tc := range cases {
		if got := validOf(t, tc.schema, tc.instance); got != tc.want {
			t.Errorf("%s with %s: valid=%v, want %v", tc.schema, tc.instance, got, tc.want)
		}
	}
	if _, err := Compile([]byte(`{"maxLength":1.5}`)); err == nil {
		t.Error("fractional maxLength compiled")
	}
	if _, err := Compile([]byte(`{"maxLength":-1}`)); err == nil {
		t.Error("negative maxLength compiled")
	}
}

// TestMultipleOfLargeInstanceFast checks that multipleOf costs time linear
// in the instance's digits, including digits that are a large power of 5.
func TestMultipleOfLargeInstanceFast(t *testing.T) {
	x := new(big.Int).Exp(big.NewInt(5), big.NewInt(100000), nil).String()
	start := time.Now()
	for _, schema := range []string{`{"multipleOf":3}`, `{"multipleOf":0.0016}`, `{"multipleOf":1e-30}`} {
		s := MustCompile([]byte(schema))
		if _, err := s.Validate([]byte(x)); err != nil {
			t.Fatal(err)
		}
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("took %v", d)
	}
	s := MustCompile([]byte(`{"multipleOf":` + x + `}`))
	start = time.Now()
	if _, err := s.Validate([]byte(x)); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("divisor 5^100000 took %v", d)
	}
}

// TestDividesMatchesBigRat compares divides with big.Rat on divisors and
// instances rich in factors of 2 and 5, and on remainders beyond uint64.
func TestDividesMatchesBigRat(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	gen := func() string {
		n := new(big.Int).Exp(big.NewInt(2), big.NewInt(int64(r.IntN(70))), nil)
		n.Mul(n, new(big.Int).Exp(big.NewInt(5), big.NewInt(int64(r.IntN(70))), nil))
		n.Mul(n, big.NewInt(int64(r.IntN(50)+1)))
		if r.IntN(4) == 0 {
			n.Mul(n, new(big.Int).Lsh(big.NewInt(int64(r.IntN(1000)+1)), 80))
		}
		return n.String() + "e" + strconv.Itoa(r.IntN(81)-40)
	}
	for range 20000 {
		as, bs := gen(), gen()
		ra, _ := new(big.Rat).SetString(as)
		rb, _ := new(big.Rat).SetString(bs)
		da, _ := parseDecimal(as)
		db, _ := parseDecimal(bs)
		want := new(big.Rat).Quo(ra, rb).IsInt()
		if got, ok := newDivisor(db).divides(da); !ok || got != want {
			t.Fatalf("divides(%s, %s) = %v; want %v", as, bs, got, want)
		}
	}
}

// TestUniqueItemsNaNLinear checks that a NaN item keeps uniqueItems
// linear, and that NaN equals nothing, itself included.
func TestUniqueItemsNaNLinear(t *testing.T) {
	arr := []any{math.NaN()}
	for i := range 50000 {
		arr = append(arr, float64(i))
	}
	s := MustCompile([]byte(`{"uniqueItems":true}`))
	start := time.Now()
	res, err := s.ValidateValue(arr)
	if err != nil || !res.Valid {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("took %v", d)
	}
	for _, tc := range []struct {
		arr  []any
		want string
	}{
		{[]any{math.NaN(), math.NaN(), json.Number("NaN")}, ""},
		{[]any{[]any{math.NaN()}, []any{math.NaN()}}, ""},
		{[]any{math.NaN(), float64(1), json.Number("1.0")}, "array items 1 and 2 are not unique"},
		{[]any{struct{}{}, float64(1), struct{}{}, float64(1)}, "array items 0 and 2 are not unique"},
		{[]any{float64(1), struct{}{}, float64(1), struct{}{}}, "array items 0 and 2 are not unique"},
		{[]any{json.Number("x"), json.Number("x")}, "array items 0 and 1 are not unique"},
		{[]any{uint64(math.MaxUint64), json.Number("18446744073709551615")}, "array items 0 and 1 are not unique"},
	} {
		res, err := s.ValidateValue(tc.arr)
		if err != nil {
			t.Fatal(err)
		}
		got := ""
		if !res.Valid {
			got = res.Errors[0].Message
		}
		if got != tc.want {
			t.Errorf("%#v: got %q, want %q", tc.arr, got, tc.want)
		}
	}
}

// TestValidateValueGoNumbers checks that every Go integer kind, float32 and
// *big.Int count as JSON numbers, including integers beyond int64 and
// uint64.
func TestValidateValueGoNumbers(t *testing.T) {
	integer := MustCompile([]byte(`{"type":"integer","minimum":0}`))
	str := MustCompile([]byte(`{"type":"string"}`))
	beyond, _ := new(big.Int).SetString("123456789012345678901234567890", 10)
	for _, v := range []any{int8(1), int16(1), int32(1), int64(1), int(1), uint(1), uint8(1),
		uint16(1), uint32(1), uint64(1), uint64(math.MaxUint64), uint64(1) << 63,
		float32(2), beyond, json.Number("123456789012345678901234567890")} {
		if res, _ := integer.ValidateValue(v); !res.Valid {
			t.Errorf("%T %v: not a non-negative integer: %v", v, v, res.Errors)
		}
		if res, _ := str.ValidateValue(v); res.Valid {
			t.Errorf("%T %v: matched type string", v, v)
		}
	}
	if res, _ := MustCompile([]byte(`{"maximum":18446744073709551614}`)).ValidateValue(uint64(math.MaxUint64)); res.Valid {
		t.Error("uint64 max passed maximum 2^64-2")
	}
	maxU := uint64(math.MaxUint64)
	for schema, valid := range map[string]bool{
		`{"enum":[18446744073709551615]}`:                  true,
		`{"const":18446744073709551615}`:                   true,
		`{"const":18446744073709551614}`:                   false,
		`{"multipleOf":5}`:                                 true,
		`{"multipleOf":2}`:                                 false,
		`{"minimum":18446744073709551615}`:                 true,
		`{"exclusiveMinimum":18446744073709551615}`:        false,
		`{"maximum":1.8446744073709551615e19}`:             true,
		`{"type":"integer","maximum":9223372036854775807}`: false,
	} {
		if res, _ := MustCompile([]byte(schema)).ValidateValue(maxU); res.Valid != valid {
			t.Errorf("%s with uint64 max: valid=%v, want %v", schema, res.Valid, valid)
		}
	}
	if res, _ := integer.ValidateValue(float32(1.5)); res.Valid {
		t.Error("float32 1.5 passed type integer")
	}
	if res, _ := integer.ValidateValue((*big.Int)(nil)); res.Valid {
		t.Error("nil *big.Int passed type integer")
	}
}

// TestHugeExponentsExact checks that numbers with exponents beyond the
// int64 range are compared exactly: equal values written differently are
// equal, and values in the same direction are ordered.
func TestHugeExponentsExact(t *testing.T) {
	for _, tc := range []struct {
		schema, inst string
		valid        bool
	}{
		{`{"const":1e1000000000000000000}`, `10e999999999999999999`, true},
		{`{"const":1e1000000000000000000}`, `0.1e1000000000000000001`, true},
		{`{"const":1e1000000000000000000}`, `1e1000000000000000001`, false},
		{`{"enum":[1e1000000000000000000]}`, `100e999999999999999998`, true},
		{`{"uniqueItems":true}`, `[1e1000000000000000000, 10e999999999999999999]`, false},
		{`{"uniqueItems":true}`, `[1e1000000000000000000, 2e1000000000000000000]`, true},
		{`{"maximum":2e1000000000000000000}`, `1e1000000000000000000`, true},
		{`{"maximum":2e1000000000000000000}`, `3e1000000000000000000`, false},
		{`{"minimum":1e1000000000000000000}`, `1e1000000000000000001`, true},
		{`{"maximum":1e-1000000000000000000}`, `1e-1000000000000000001`, true},
		{`{"minimum":1e-1000000000000000000}`, `1e-1000000000000000001`, false},
		{`{"const":1e1000000000000000}`, `0.1e1000000000000001`, true},
		{`{"maximum":1e1000000000000000}`, `0.01e1000000000000002`, true},
		{`{"maximum":1e1000000000000000}`, `0.11e1000000000000002`, false},
		{`{"multipleOf":8e999999999999999}`, `1e1000000000000001`, false},
		{`{"multipleOf":8e999999999999999}`, `1e1000000000000003`, true},
		{`{"multipleOf":1e999999999999990}`, `1e1000000000000001`, true},
		{`{"multipleOf":3}`, `3e10000000000000000000000`, true},
		{`{"multipleOf":3}`, `2e10000000000000000000000`, false},
	} {
		if got := validOf(t, tc.schema, tc.inst); got != tc.valid {
			t.Errorf("%s with %s: valid=%v, want %v", tc.schema, tc.inst, got, tc.valid)
		}
	}
	for _, tc := range []struct {
		neg   bool
		mag   string
		delta int64
		want  string
	}{
		{false, "999999999999999999", 1, "1000000000000000000"},
		{false, "1000000000000000000", -1, "999999999999999999"},
		{true, "1000000000000000000", 5, "-999999999999999995"},
		{true, "999999999999999999", -1, "-1000000000000000000"},
		{false, "5", -7, "-2"},
		{false, "", 0, "0"},
	} {
		neg, mag := addSmall(tc.neg, tc.mag, tc.delta)
		got := mag
		if got == "" {
			got = "0"
		}
		if neg {
			got = "-" + got
		}
		if got != tc.want {
			t.Errorf("addSmall(%v, %s, %d) = %s, want %s", tc.neg, tc.mag, tc.delta, got, tc.want)
		}
	}
}
