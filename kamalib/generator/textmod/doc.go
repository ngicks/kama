// Package textmod edits the values of tagged var and const specs in place.
//
// A spec carries the directive //kama:textmod <id>. A step gives a
// text/template per id. The generator runs the template on the current
// value of the spec, parses the output back as a value of the same kind,
// and writes the new literal over the old one. Every other byte of the
// file stays as it was. The generator puts no generated mark on a file.
//
// # Directive
//
// The directive is
//
//	//kama:textmod <id>[, format="ver"|"date-time"]
//
// and its args are Starlark expressions as package
// github.com/ngicks/kama/kamalib/pkg/dircmnt/expr parses them. The id is
// the first positional item: a bare word, as in version, or a string, as
// in "app-version". A bare app-version parses as a subtraction and is an
// error that says to quote it. A keyword other than format is warned about
// with ErrUnknownKey.
//
// The directive stands in one of these places:
//
//   - the doc comment of a declaration of one spec, as in const X = 1;
//   - the doc comment of a spec inside a group;
//   - the line comment at the end of a spec.
//
// Package-level declarations and declarations inside function bodies both
// count. A directive anywhere else is an error wrapping ErrDirective: on a
// type, a func, a field, a free-floating comment, or the doc comment of a
// group of several specs. A spec tagged twice is an error too. A directive
// in a file that carries the header of generated code or a //kama:generated
// mark of any generator is an error. Such a file is the output of a
// generator.
//
// # Values
//
// The tagged spec declares exactly one name with exactly one value, and
// that value is a basic literal: an integer, a float, an interpreted or raw
// string, or the identifier true or false. A leading - or + of a number is
// part of the literal. The spec may be typed, as in const X int8 = 3 or
// var D time.Duration = 5, or untyped. Each of these is an error wrapping
// ErrTarget, naming the position and the reason: several names, no value
// (an iota continuation line, or a var without an initializer), iota, any
// other expression (a call, a binary expression, a composite literal, an
// identifier), a rune or imaginary literal, and a format on a value that
// is not a string.
//
// The kind of a value is int, float, string or bool. The type of the spec,
// as the loaded packages hold it, gives the kind when its underlying type
// is basic. A typed float spelled as an integer is a float, and a typed
// integer spelled as a float is an integer. Otherwise the literal gives the
// kind. When the loaded packages hold no type for the spec, a predeclared
// type spelled on the spec stands in.
//
// A string takes a format:
//
//   - ver: the value is an exver.Version of
//     github.com/ngicks/go-common/exver. The output of the template must
//     parse with exver, and the String of the parsed version is written.
//   - date-time: the value is a time.Time parsed as RFC 3339. The output
//     must parse as RFC 3339, and it is written in time.RFC3339Nano. A
//     template prints a time.Time in another layout. It ends with
//     rfc3339.
//
// # Templates
//
// A template runs on a [Data]: the id, the name and the package of the
// spec, its file, its kind and format, the literal as the source spells
// it, the current value and the vars of the step. The current value is an
// int64, or a *big.Int when it does not fit, for int; a float64 for float;
// a string, an exver.Version or a time.Time for string; a bool for bool. A
// template reading a key that .Vars lacks fails.
//
// The generator drops one trailing newline of a template source. A YAML
// block scalar then gives the same template as a quoted one. It then parses the
// output in the kind and format of the value:
//
//   - int: a Go integer literal with an optional sign, after white space is
//     trimmed. A typed integer holds the value. int, uint and uintptr are
//     taken as 64 bits wide.
//   - float: a Go integer or float literal with an optional sign, after
//     white space is trimmed. A float32 or a float64 holds it. A decimal
//     integer on a spec spelling no type is written with .0 appended. The
//     spec then stays a float. Another integer literal there is an error.
//   - bool: true or false, after white space is trimmed.
//   - string: the output itself, white space included, unless it has a
//     format.
//
// An output that does not parse is an error wrapping ErrMod that names the
// id. A new value equal to the current one keeps the literal as the source
// spells it. 0x10 stays 0x10, and an escape in a string stays. Otherwise
// a number is written as the output spells it, and a bool as true or
// false. An interpreted string is written with strconv.Quote. A raw string
// stays raw unless the new value holds a backquote or a carriage return;
// then it becomes an interpreted string.
//
// The functions a template calls are those of text/template, those of
// [BuiltinFuncs], and those other packages add with [RegisterFuncs]. No
// built-in function reads the clock, the environment or a file: such a
// value comes through vars. [BuiltinFuncs] documents the table. exver has
// no bump helper. A template bumps a version with the functions setting
// and dropping its components.
//
// # Ids
//
// An id names one spec of the loaded packages. The packages are ordered by
// path, their files by name, and the directives of a file by position. The
// first spec in that order takes the mod of its id. An id tagged on several
// specs is warned about with ErrDuplicateId, naming every position. A mod
// whose id no spec carries is warned about with ErrUnusedMod. A spec whose
// id has no mod stays as it is, without a warning.
//
// # In-place output
//
// The generator reads each file of the loaded packages under the root of
// the output from Env.Out and parses it again. It takes positions from
// the content as it is. It splices every edit of a file into that
// content and yields the file once, as a base.Whole with Verbatim set. A
// file whose bytes do not change yields nothing. The framework stages a
// verbatim Whole byte for byte. A file that is not gofmt-clean stays as it
// was outside the edits.
//
// A mod setting a value from vars is idempotent: a second run with the same
// param changes nothing. A mod computing from the current value, such as
// one adding to a number or bumping a version, changes the file on every
// run.
//
// # Proc
//
// The proc, the workflow step kind textmod.replace, takes the packages a
// packages.load step loaded as its in and the output as its out. Its param
// has two keys:
//
//   - mods: a map from an id to the source of its template.
//   - vars: a map of caller values, which a template reads as .Vars.
//
// A key of the param that names neither is warned about. The proc parses
// every template when the param is set. A dry run then reports a template
// that does not parse. The proc has no stale check. The generator writes
// no generated mark.
//
//	version: 1
//	steps:
//	  root:
//	    proc: overlayfs.open
//	  load:
//	    proc: packages.load
//	    in: root
//	  replace:
//	    proc: textmod.replace
//	    in: load
//	    out: root
//	    param:
//	      vars:
//	        version: v1.2.0
//	      mods:
//	        version: "{{ .Vars.version }}"
//	        build: "{{ add .Value 1 }}"
//	  final:
//	    proc: overlayfs.copy_down
//	    in: root
//	    after: [replace]
//
// # Function registry
//
// [RegisterFuncs] adds a [FuncProvider] under a name from an init function.
// A run calls each provider once with its [Run], the loaded packages and
// the vars of the run. A provider binds its functions to the data of that
// run. A function name defined twice fails the run.
package textmod
