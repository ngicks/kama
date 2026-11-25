package rawjson

import (
	"math/big"
	"strings"
)

// decimal is a JSON number as the value 0.digits * 10^point.
//
// The exponent of a JSON number can be arbitrarily large, so the value is never expanded into
// its digits or into a big.Rat or big.Float: a 20-byte text such as 1e9999999999 would take
// gigabytes, and a big.Float of bounded exponent rounds it to infinity or to zero. Every
// operation works on the digits and the exponent, so its cost grows with the length of the
// text alone.
type decimal struct {
	neg bool
	// digits has no leading and no trailing zero. It is empty for zero.
	digits string
	point  *big.Int
}

// parseDecimal parses num, the text of a JSON number. ok is false when num is not one. Zero has
// neg false whatever its sign.
func parseDecimal(num string) (decimal, bool) {
	mantissa, expText, hasExp := strings.Cut(strings.ToLower(num), "e")

	neg := false
	if rest, ok := strings.CutPrefix(mantissa, "-"); ok {
		neg, mantissa = true, rest
	}

	whole, frac, hasFrac := strings.Cut(mantissa, ".")
	if !allDigits(whole) || hasFrac && !allDigits(frac) {
		return decimal{}, false
	}

	point := new(big.Int)

	if hasExp {
		if _, ok := point.SetString(strings.TrimPrefix(expText, "+"), 10); !ok ||
			!allDigits(strings.TrimLeft(expText, "+-")) {
			return decimal{}, false
		}
	}

	all := whole + frac
	digits := strings.TrimLeft(all, "0")

	if digits == "" {
		return decimal{point: new(big.Int)}, true
	}

	// Leading zeros of the whole part move the point left; those of the fraction are already
	// after it.
	point.Add(point, big.NewInt(int64(len(whole)-(len(all)-len(digits)))))

	return decimal{neg: neg, digits: strings.TrimRight(digits, "0"), point: point}, true
}

func allDigits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

func (d decimal) sign() int {
	switch {
	case d.digits == "":
		return 0
	case d.neg:
		return -1
	}

	return 1
}

// cmp compares d to e as -1, 0 or +1.
func (d decimal) cmp(e decimal) int {
	if sd, se := d.sign(), e.sign(); sd != se || sd == 0 {
		return compareInts(sd, se)
	}

	c := d.point.Cmp(e.point)
	if c == 0 {
		// Both digit strings start right after the point and end in no zero, so their
		// lexical order is the order of the values.
		c = strings.Compare(d.digits, e.digits)
	}

	if d.neg {
		return -c
	}

	return c
}

// isInt reports whether d has no fraction. The digits end in no zero, so a point before the
// last digit leaves one.
func (d decimal) isInt() bool {
	return d.digits == "" || d.point.Cmp(big.NewInt(int64(len(d.digits)))) >= 0
}

// mantissa returns d as m * 10^exp for an integer m >= 0.
func (d decimal) mantissa() (*big.Int, *big.Int) {
	m, _ := new(big.Int).SetString(d.digits, 10)
	if m == nil {
		m = new(big.Int)
	}

	return m, new(big.Int).Sub(d.point, big.NewInt(int64(len(d.digits))))
}

// divides reports whether d, which must not be zero, divides v to an integer.
//
// With v = m * 10^e and d = n * 10^f, v / d is an integer when n divides m * 10^(e-f). For
// e-f >= 0, 10^(e-f) is taken modulo n, which modular exponentiation does in time logarithmic in
// e-f. For e-f < 0, n * 10^(f-e) exceeds m when f-e reaches the number of digits of m, so the
// power stays as small as the text of v.
func (d decimal) divides(v decimal) bool {
	if v.digits == "" {
		return true
	}

	m, e := v.mantissa()
	n, f := d.mantissa()
	k := e.Sub(e, f)

	if k.Sign() >= 0 {
		p := new(big.Int).Exp(big.NewInt(10), k, n)
		p.Mul(p, m)

		return p.Mod(p, n).Sign() == 0
	}

	k.Neg(k)

	if k.Cmp(big.NewInt(int64(len(v.digits)))) >= 0 {
		return false
	}

	div := new(big.Int).Exp(big.NewInt(10), k, nil)
	div.Mul(div, n)

	return new(big.Int).Mod(m, div).Sign() == 0
}

// compareNumbers compares the JSON number texts a and b. ok is false when either is not one.
func compareNumbers(a, b string) (int, bool) {
	da, okA := parseDecimal(a)
	db, okB := parseDecimal(b)

	if !okA || !okB {
		return 0, false
	}

	return da.cmp(db), true
}

func isInteger(text string) bool {
	d, ok := parseDecimal(text)
	return ok && d.isInt()
}

// multiple reports whether num divides v to an integer. Zero divides nothing.
func multiple(v, num string) bool {
	dv, okV := parseDecimal(v)
	dn, okN := parseDecimal(num)

	return okV && okN && dn.sign() != 0 && dn.divides(dv)
}
