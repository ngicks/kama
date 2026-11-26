// Package ext holds the question package fieldtree asks the type supports
// of genext: how a Go text/template reaches the values a type holds. A
// type support implements Reacher next to genext.Holder, genext.Container
// or genext.Composite, and package fieldtree draws a line per held value
// from its answer.
//
// Package genext/opt implements Reacher for the types of
// github.com/ngicks/option, and package genext/std for sql.Null[T], the
// null types of database/sql and unique.Handle[T]. A text extension file
// answers through its section fieldtree, as TextSection describes.
//
// A claimed type whose support does not implement Reacher still gets a
// line per role. The line names the role and the held type, without a
// template expression.
package ext

import (
	"go/types"

	"github.com/ngicks/kama/kamalib/generator/genext"
)

// Reacher is a genext.TypeSupport telling how a template reaches the values
// a type holds.
type Reacher interface {
	genext.TypeSupport
	// Reach returns how a template reaches the value of role that a value of
	// type t holds. v is the template expression of the value, as .Timeout,
	// and empty for the dot itself.
	//
	// access is the template expression of the held value built on v, as
	// .Timeout.Value. An element of many takes the index i or the key key,
	// as index .Items.Options i. note tells the reader when the value is
	// there, as "set only when .Timeout.IsSome". key is what the held value
	// adds to the key path of v in the serialized form: empty when it
	// encodes alone, .left for a JSON member left and [i] for an element.
	// ok is false for a type the support does not claim and for a role it
	// does not give.
	Reach(t types.Type, role, v string) (access, note, key string, ok bool)
}
