// Package shareda holds context keys generated from the template that
// sharedb is also generated from.
//
//kama:option-file contextkey=../templates/shared.yml
package shareda

type keyTy string

// keyTypeFunc returns a pointer so that keys of entries sharing a type
// still differ.
func keyTypeFunc(name string) *keyTy {
	return (*keyTy)(&name)
}
