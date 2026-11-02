// Package inline holds context keys generated into the directory of their
// template, with the dir param left at its default.
package inline

//go:generate go run ../../generate -spec spec.yaml

type keyTy string

// keyTypeFunc returns a pointer so that keys of entries sharing a type
// still differ.
func keyTypeFunc(name string) *keyTy {
	return (*keyTy)(&name)
}
