// Package jsontemplate holds context keys generated from a JSON template
// in another directory.
//
//kama:option-file contextkey=../templates/json_keys.json
package jsontemplate

type keyTy string

// keyTypeFunc returns a pointer so that keys of entries sharing a type
// still differ.
func keyTypeFunc(name string) *keyTy {
	return (*keyTy)(&name)
}
