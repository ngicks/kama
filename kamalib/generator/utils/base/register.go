package base

import "github.com/ngicks/kama/kamalib/pkg/genmark"

// MustRegister registers name, the name a generator marks its declarations
// with, in genmark.DefaultRegistry. It panics on error, since it is meant
// for init functions, where a duplicate or invalid name is a programming
// error.
func MustRegister(name string) {
	if err := genmark.DefaultRegistry.Register(name); err != nil {
		panic(err)
	}
}
