// Package tomltemplate holds context keys generated from a TOML template
// in another directory.
//
//kama:option-file contextkey=../templates/toml_keys.toml
package tomltemplate

type keyTy string

// keyTypeFunc returns a pointer so that keys of entries sharing a type
// still differ.
func keyTypeFunc(name string) *keyTy {
	return (*keyTy)(&name)
}
