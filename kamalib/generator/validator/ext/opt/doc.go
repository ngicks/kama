// Package opt holds the type support of the option types of
// github.com/ngicks/option and registers it into the default registry of
// package ext in its init function. A program importing package validator
// imports this package with it.
//
// Support tells the generator and the validators about these types. JSON
// locations below name the members the types encode as:
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
//   - either.Either holds its left value, role "left", when IsLeft reports
//     true and its right value, role "right", when IsRight does. It encodes
//     as an object tagging the value with the member left or right, which
//     locates the value.
//   - either.Result holds its value, role "ok", when IsOk reports true,
//     located at the member ok it encodes as. Its error is no value to
//     validate.
//   - either.UntaggedEither and either.UntaggedResult hold the values of
//     Either and Result under the same roles. They encode the value alone,
//     so their values add no path segment.
//   - tuple.Tuple2 to tuple.Tuple16 hold their fields V1 to VN, roles "v1"
//     to "vN", always. A tuple encodes as an object with the members V1 to
//     VN, which locate the fields.
//
// A rule rejecting an opt.Option or an und.Und applies to the value it
// holds, as it does to the value a pointer points to. An elastic.Elastic
// holds no single value, and neither do the types of packages either and
// tuple, so a rule applies to them as a whole only. The nested walk and the
// selection of targets go into the values all of them hold.
package opt
