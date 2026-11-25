// Package deepkeys holds context keys generated into a nested directory
// whose name differs from the package name.
//
//kama:option-file contextkey=../../templates/nested.yaml
package deepkeys

type keyTy string

// keyTypeFunc returns a pointer so that keys of entries sharing a type
// still differ.
func keyTypeFunc(name string) *keyTy {
	return (*keyTy)(&name)
}
