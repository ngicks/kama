package fieldtree

import (
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/golden"

	"github.com/ngicks/kama/kamalib/generator/genext"
	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
)

const modulePath = "github.com/ngicks/kama/kamalib/generator/fieldtree/"

// gen loads files as gentest.Load does and runs Generate on the packages
// with the default registry.
func gen(t *testing.T, files map[string]string, types map[string]Target) (Texts, []error, error) {
	t.Helper()

	return genWith(t, files, types, genext.DefaultRegistry())
}

func genWith(
	t *testing.T,
	files map[string]string,
	types map[string]Target,
	reg genext.Supports,
) (Texts, []error, error) {
	t.Helper()

	pkgs := gentest.Load(t, files)

	var warnings []error

	texts, err := Generate(Input{
		Pkgs:     base.SourcePackages(pkgs),
		Types:    types,
		Registry: reg,
	}, func(w error) { warnings = append(warnings, w) })

	return texts, warnings, err
}

const configSrc = `package p

import (
	"time"

	"github.com/ngicks/option/opt"
)

//kama:fieldtree config
type Config struct {
	// Addr is the listen address. It defaults to :8080.
	Addr string ` + "`json:\"addr\"`" + `
	Port int ` + "`json:\"port\"`" + ` // listen port
	// Server configures the server.
	Server Server ` + "`json:\"server\"`" + `
	Labels map[string]string ` + "`json:\"labels\"`" + ` // labels
	Hosts []string ` + "`json:\"hosts\"`" + ` // hosts
	Timeout opt.Option[int] ` + "`json:\"timeout\"`" + `
	Upstreams []Upstream ` + "`json:\"upstreams\"`" + `
	Tls opt.Option[Tls] ` + "`json:\"tls\"`" + `
	Root *Node ` + "`json:\"root\"`" + `
	Grid [][2]int ` + "`json:\"grid\"`" + `
	Since time.Time ` + "`json:\"since\"`" + `
	Raw []byte ` + "`json:\"raw\"`" + `
	Any any ` + "`json:\"any\"`" + `
	Hook func() ` + "`json:\"-\"`" + `
	Ch chan int
	Base
	secret string
}

type Server struct {
	TLS     bool ` + "`json:\"tls\"`" + ` // enable TLS
	Timeout int  ` + "`json:\"timeout\"`" + ` // timeout seconds
}

type Upstream struct {
	Name string ` + "`json:\"name\"`" + ` // upstream name
	Weight int ` + "`json:\"weight,omitempty\"`" + `
}

type Tls struct {
	Cert string ` + "`json:\"cert\"`" + ` // certificate file
}

// Node is a recursive type.
type Node struct {
	Name     string  ` + "`json:\"name\"`" + `
	Children []*Node ` + "`json:\"children\"`" + `
}

type Base struct {
	Version int ` + "`json:\"version\"`" + ` // config version
}
`

func TestConfigTree(t *testing.T) {
	texts, warnings, err := gen(t, map[string]string{"p/p.go": configSrc}, nil)
	assert.NilError(t, err)
	assert.Equal(t, len(warnings), 0, "%v", warnings)
	golden.Assert(t, texts["config"], "config.golden")
}
