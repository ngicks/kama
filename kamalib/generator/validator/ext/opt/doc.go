// Package opt holds the type support of the option types of
// github.com/ngicks/option and registers it into the default registry of
// package ext in its init function. A program importing package validator
// imports this package with it.
//
// Support tells the generator and the validators about these types:
//
//   - opt.Option holds one value, role "t", when IsSome reports true. It is
//     present when some and zero when none.
//   - und.Und holds one value, role "t", when IsDefined reports true, null
//     included. It is present when not undefined, zero when undefined and
//     null by IsNull.
//   - elastic.Elastic holds elements, role "t", the opt.Option per element
//     that Options returns, located by index. A null element is a none
//     opt.Option, which holds nothing to validate. It is present, zero and
//     null as und.Und is.
//
// A rule rejecting an opt.Option or an und.Und applies to the value it
// holds, as it does to the value a pointer points to. An elastic.Elastic
// holds no single value, so a rule applies to it as a whole only.
package opt
