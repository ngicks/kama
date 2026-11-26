package fieldtree

import (
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/golden"
)

// docSrc is the example of the package doc. doc.golden holds its tree,
// which the package doc repeats.
const docSrc = `package p

import "github.com/ngicks/option/opt"

//kama:fieldtree config
type Config struct {
	// Addr is the listen address.
	Addr  string            ` + "`json:\"addr\"`" + `
	Users []User            ` + "`json:\"users\"`" + ` // users allowed in
	Env   map[string]string ` + "`json:\"env\"`" + `   // environment
	TLS   opt.Option[TLS]   ` + "`json:\"tls\"`" + `   // TLS settings
	Root  *Dir              ` + "`json:\"root\"`" + `  // served directory
}

type User struct {
	Name string ` + "`json:\"name\"`" + ` // login name
}

type TLS struct {
	Cert string ` + "`json:\"cert\"`" + ` // certificate file
}

type Dir struct {
	Path string ` + "`json:\"path\"`" + ` // path on disk
	Sub  []Dir  ` + "`json:\"sub\"`" + `  // subdirectories
}
`

func TestDocExample(t *testing.T) {
	texts, warnings, err := gen(t, map[string]string{"p/p.go": docSrc}, nil)
	assert.NilError(t, err)
	assert.Equal(t, len(warnings), 0, "%v", warnings)
	golden.Assert(t, texts["config"], "doc.golden")
}
