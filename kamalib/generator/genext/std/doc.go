// Package std registers the type supports of types of the standard library
// in the default registry of package genext. Package validator imports it,
// so every run of the generator knows these types.
//
// Types holding a value. The nested validation and the role t reach the
// value, and a rule the type itself does not take applies to the value
// under its guard, as for an opt.Option:
//
//	type                     value              guard           required, null
//	sql.Null[T]              V                  Valid           Valid
//	sql.NullString           String             Valid           Valid
//	sql.NullInt64            Int64              Valid           Valid
//	sql.NullInt32            Int32              Valid           Valid
//	sql.NullInt16            Int16              Valid           Valid
//	sql.NullByte             Byte               Valid           Valid
//	sql.NullBool             Bool               Valid           Valid
//	sql.NullFloat64          Float64            Valid           Valid
//	sql.NullTime             Time               Valid           Valid
//	unique.Handle[T]         Value()            not the zero Handle
//
// The zero unique.Handle holds nothing, and its Value panics, so the guard
// leaves it out.
//
// Types the rules understand:
//
//	type                     rule                          operand
//	time.Time                cmp                           RFC 3339 string
//	time.Duration            cmp                           duration string
//	*big.Int                 cmp                           integer
//	*big.Int                 multipleof                    positive integer
//	*big.Float, *big.Rat     cmp                           number
//	netip.Addr, netip.Prefix zero, required                tested by IsValid
//
// A time.Time compares by Compare, so that values naming the same instant
// in other locations are equal. The operand is spelled as time.Unix and
// parsed once, at generation. zero and required test a time.Time by its
// method IsZero, as for any type declaring IsZero. A number compared with
// a time.Duration and multipleof on a time.Duration take nanoseconds, as
// for any integer type, so cmp(ge="1s", le=300000000000) mixes both. A
// *big.Int, *big.Float
// or *big.Rat compares by Cmp with the exact value of the literal, which a
// package-level variable holds. A *big.Float compares exactly, so that
// cmp(eq=0.1) holds for no finite value. A nil pointer of math/big passes
// cmp and multipleof, as a nil pointer passes the rules on what it points
// to. The value types of math/big are not supported, since the package
// does not support copying them. An operand a type does not read, such as
// cmp(ge="soon") on a time.Time, is a generation error wrapping ext.ErrArgs.
package std
