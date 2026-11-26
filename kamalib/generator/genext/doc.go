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
package genext
