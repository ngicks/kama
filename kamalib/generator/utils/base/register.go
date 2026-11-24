package base

import "github.com/ngicks/kama/kamalib/pkg/genmark"

// ToolName is the tool name of kama: the prefix of the directive comments
// kama reads and writes, as in //kama:generated.
const ToolName = "kama"

// DefaultRegistry is the Registry kama generators register their names in.
var DefaultRegistry = genmark.NewRegistry(ToolName)

// Marker returns the directive comment marking a declaration as emitted by
// the kama generator name.
func Marker(name string) string {
	return genmark.Marker(ToolName, name)
}

// MustRegister registers name, the name a generator marks its declarations
// with, in DefaultRegistry. It panics on error, since it is meant for init
// functions, where a duplicate or invalid name is a programming error.
func MustRegister(name string) {
	if err := DefaultRegistry.Register(name); err != nil {
		panic(err)
	}
}
