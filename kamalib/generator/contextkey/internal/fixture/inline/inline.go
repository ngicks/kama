// Package inline holds context keys generated from kama_contextkey.yaml,
// the template file a package naming none has.
package inline

//go:generate go run ../../../../../cmd/kama-simple --spec spec.yaml

type keyTy string

// keyTypeFunc returns a pointer so that keys of entries sharing a type
// still differ.
func keyTypeFunc(name string) *keyTy {
	return (*keyTy)(&name)
}
