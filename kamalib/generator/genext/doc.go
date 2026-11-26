// Package genext holds what generators of this repository know about the
// values a named type holds: the type supports, the roles of held values
// and the registry of supports every generator shares.
//
// # Type supports
//
// A generator sees through the fields of a struct, the element of a slice,
// an array or a pointer, and the keys and values of a map. It cannot see
// what an opaque named type holds, such as an opt.Option or a container
// of a third party package. A TypeSupport tells it. A support implements
// one or more of these interfaces, and each method reports false for a
// type the support does not claim:
//
//   - Holder: the one value a type may hold, under a guard, as opt.Option
//     holds one when it is some.
//   - Container: the elements of a type, ranged over and located by index
//     or key, as elastic.Elastic holds its elements.
//   - Composite: several values side by side, each of its own type, under
//     its own guard and located by its own segment, as either.Either holds
//     a left or a right value and tuple.Tuple2 holds two.
//
// The answers are code: a support returns a Gen, the text of a guard, of a
// value or of a range expression, built from the Gen of the value it is
// asked about. A generator places that text in the code it writes.
//
// # Roles and segments
//
// Each held value, each element and each value of a Composite has a role
// name, such as "t", "l" or "v1". A role names the value among the values
// a type holds, so that a directive can address it. The role names a
// support gives are part of its API. A role name is a Go identifier. Roles
// lists the roles of a type: e for the elements of a slice or an array and
// for the value a pointer points to, k and v for the keys and the values
// of a map, and the roles of the supports. RoleValue tells how the values
// of a role are reached: their type, the guard, the value expression or
// the range expression, and the Segment locating them in a path, such as
// an index or the name of a JSON member.
//
// HeldValue, HolderOf, ContainerOf, CompositeOf and Dependants ask the
// supports of a Supports value. Dependants lists the types a type holds,
// so that a generator selecting the types to generate for walks into
// them.
//
// # Registry
//
// A generator asks the supports at the point it needs an answer, in the
// order they were registered, and the first support claiming a type
// answers. It keeps no answer. RegisterTypeSupport adds a support to the
// default registry, and DefaultRegistry returns a clone of it, so that a
// run changing its registry changes no other run. RegisterTypeSupport
// panics on a name that is registered already, since two extension
// packages must not claim one name.
//
// # One package for several generators
//
// An extension package implements the interfaces of this package once and
// registers its supports in its init function. Every generator reading the
// default registry then knows the types. A generator with questions of its
// own about a type defines them as optional interfaces embedding
// TypeSupport in its own package, and asks them through SupportsOf. The
// extension package implements those interfaces on the same support type,
// in the same package:
//
//	package myext
//
//	func init() { genext.RegisterTypeSupport(dequeSupport{}) }
//
//	type dequeSupport struct{}
//
//	func (dequeSupport) Name() string { return "deque" }
//
//	// Elements implements genext.Container.
//	func (dequeSupport) Elements(
//		t types.Type,
//		v genext.Gen,
//	) (string, types.Type, genext.Gen, genext.Segment, bool) {
//		if !typequery.IsNamed(t, "github.com/gammazero/deque", "Deque") {
//			return "", nil, nil, 0, false
//		}
//
//		return "e", typequery.TypeArg(t), lang.Expr("%s.Iter()", v), genext.SegmentIndex, true
//	}
//
//	// Length implements ext.LengthTester of the validator.
//	func (dequeSupport) Length(t types.Type, v genext.Gen) (genext.Gen, bool) {
//		if !typequery.IsNamed(t, "github.com/gammazero/deque", "Deque") {
//			return nil, false
//		}
//
//		return lang.Expr("%s.Len()", v), true
//	}
//
// A program imports the extension package with a blank import. The package
// imports the packages of the generators whose optional interfaces it
// implements. A generator never imports an extension package other than
// by a blank import, so no import cycle arises. Package opt holds the
// support of github.com/ngicks/option, and package std holds the supports
// of types of the standard library.
//
// # Text extension files
//
// A text extension file describes type supports with data and
// text/template expressions, so that a type gets a support without Go
// code. LoadText builds supports from loaded files, one per described type
// and one per generator section of it, and they implement the same
// interfaces a Go support does. A generator puts them into the registry of
// its run. The validator reads the files its proc param extension_files
// names.
//
// A file is TOML, YAML or JSON, read as option files are. It holds
// version, which is 1, and these tables:
//
//   - imports maps a name to the import path of a package outside the
//     standard library. A template refers to the package by
//     {{pkg "<name>"}}.
//   - helpers maps a name to a helper declaration. decl is the template of
//     exactly one package-level function, variable or constant declaration
//     named {{.Name}}. A template calls the helper by {{helper "<name>"}},
//     which gives the name the declaration takes.
//   - types maps the import path of a package and the name of a type,
//     joined by a dot, to the description of the type: holder, container,
//     composite and generator.
//
// A holder describes a Holder: role, type, guard and value. A container
// describes a Container: role, type, over, yields and segment. yields is
// values when over yields the elements alone, as an iter.Seq does, and
// pairs when it yields an index or a key with each element, as an
// iter.Seq2 does. segment is index or key for pairs, what the first value
// locates an element by. A container yielding values locates its elements
// by their count. A composite lists its parts, each with role, type,
// guard, value, segment and label. segment is none, index, key or field,
// and label is the template of a Go expression of the segment, as
// "\"left\"" for a field. An empty guard of a part stands for always.
//
// type names the type of a value by the name of a type parameter of the
// described type, as T, or by .Name, the type of the field Name or of the
// first result of the method Name. A generic type is described once, and
// the type parameter stands for the type argument of each instance.
//
// generator holds a section per generator, keyed by its name. A generator
// package registers the builder of its section with RegisterTextSection.
// The validator registers validate, whose keys package validator/ext
// documents, and fieldtree registers fieldtree, whose keys package
// fieldtree/ext documents.
//
// Templates execute with text/template while the generated code renders.
// .V is the code of the value. Three functions reach outside it:
//
//   - {{std "<import path>"}} spells a package of the standard library, as
//     {{std "strings"}}.TrimSpace({{.V}}). A path whose first element holds
//     no dot is a package of the standard library when its directory lies
//     under GOROOT/src, or when the program knows no GOROOT.
//   - {{pkg "<name>"}} spells the package imports names.
//   - {{helper "<name>"}} declares the helper of helpers and spells its
//     name. A helper calls no other helper.
//
// Each package a template names resolves through the imports of the file
// the code lands in, under an identifier no other import takes, and is
// imported only when code naming it is emitted. A file imports a package
// the generated package does not depend on under the last element of its
// path. Each helper is declared once per package, in the helper file of
// the generator, under a name the generator prefixes, as
// kamaValidateExtCount for the helper count of the validator. Every key
// but the guard of a part, and the label of a part without segment, is
// required. A missing key and a template that does not parse are errors
// when the file loads. A template that does not execute, and a name
// imports or helpers does not declare, is an error of the run. Each error names the file and the
// key path of the
// template. A key that names nothing is a warning naming its key path.
//
// The generator does not type-check the code of a template. A method the
// value lacks makes the generated code fail to compile. CheckText reports
// a described type missing from a loaded package, and a type naming no
// type parameter, field or method.
//
// This file describes a generic container, a holder and a composite of two
// parts, and tells the validator how to count and test them:
//
//	version = 1
//
//	[imports]
//	opt = "github.com/ngicks/option/opt"
//
//	[helpers.count]
//	decl = '''
//	func {{.Name}}[T any](seq {{std "iter"}}.Seq[T]) int {
//		n := 0
//		for range seq {
//			n++
//		}
//		return n
//	}
//	'''
//
//	[types."example.com/bag.Bag".container]
//	role = "v"
//	type = "T"
//	over = "{{.V}}.Values()"
//	yields = "values"
//
//	[types."example.com/bag.Bag".generator.validate.length]
//	expr = '{{helper "count"}}({{.V}}.Values())'
//
//	[types."example.com/bag.Maybe".holder]
//	role = "v"
//	type = "T"
//	guard = "{{.V}}.IsSome()"
//	value = "{{.V}}.Get()"
//
//	[types."example.com/bag.Maybe".generator.validate.present]
//	cond = "{{.V}}.IsSome()"
//	words = "none"
//
//	[types."example.com/bag.Maybe".generator.validate.zero]
//	cond = '{{pkg "opt"}}.FromOk({{.V}}.Get(), {{.V}}.IsSome()).IsNone()'
//
//	[[types."example.com/bag.Pair".composite.parts]]
//	role = "k"
//	type = "K"
//	value = "{{.V}}.Key"
//	segment = "field"
//	label = '"key"'
//
//	[[types."example.com/bag.Pair".composite.parts]]
//	role = "v"
//	type = "V"
//	guard = "{{.V}}.HasValue()"
//	value = "{{.V}}.Value"
//	segment = "field"
//	label = '"value"'
//
//	[types."example.com/bag.Label".generator.validate.zero]
//	cond = '{{std "strings"}}.TrimSpace({{.V}}.String()) == ""'
//
//	[types."example.com/bag.Label".generator.validate.compare]
//	ops = ["==", "!="]
//	operand = "string"
//	cond = "{{.V}}.String() {{.Op}} {{.Operand}}"
package genext
