package base

import "github.com/ngicks/kama/kamalib/pkg/genmark"

// ToolName is the tool name of kama: the prefix of the directive comments
// the framework reads and writes for every generator, as in
// //kama:generated.
const ToolName = "kama"

// DefaultRegistry is the Registry generators register their Ids in.
var DefaultRegistry = genmark.NewRegistry(ToolName)

// Id identifies a generator. Namespace is kama for the generators of kama
// and the own name of a third party for its generators. Name is the
// generator within the namespace, such as partial. The pair names the
// directive of the generator, its option file key, its generated mark and
// its generated files.
type Id struct {
	Namespace string
	Name      string
}

// String returns the name the generated mark gives the generator, as in
// kama:partial.
func (id Id) String() string { return id.Namespace + ":" + id.Name }

// Marker returns the directive comment marking a declaration as emitted by
// the generator, as in //kama:generated kama:partial.
func (id Id) Marker() string { return genmark.Marker(ToolName, id.String()) }

// Directive returns the directive comment of the generator without args,
// as in //kama:partial.
func (id Id) Directive() string { return "//" + id.Namespace + ":" + id.Name }

// Naming returns the Naming of the files of the generator, named
// kama_partial for kama:partial.
func (id Id) Naming() Naming { return Naming{Name: id.Namespace + "_" + id.Name} }

// ProcKind returns the kind a workflow registry knows the SourceProc of
// the generator by, as in partial.generate.
func (id Id) ProcKind() string { return id.Name + ".generate" }

// MustRegister registers id in DefaultRegistry. It panics on error, since
// it is meant for init functions, where a duplicate or invalid Id is a
// programming error.
func MustRegister(id Id) {
	if err := DefaultRegistry.Register(id.String()); err != nil {
		panic(err)
	}
}
