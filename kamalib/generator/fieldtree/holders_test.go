package fieldtree

import (
	"database/sql"
	"errors"
	"go/types"
	"strings"
	"testing"
	"text/template"
	"unique"

	"github.com/ngicks/option/either"
	"github.com/ngicks/option/elastic"
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/tuple"
	"github.com/ngicks/option/und"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/golden"

	"github.com/ngicks/kama/kamalib/generator/genext"
	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
)

// holdersSrc declares the fields of holders, which the test fills with
// values to run the accessors on.
const holdersSrc = `package p

import (
	"database/sql"
	"unique"

	"github.com/ngicks/option/either"
	"github.com/ngicks/option/elastic"
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/tuple"
	"github.com/ngicks/option/und"
)

//kama:fieldtree holders
type Holders struct {
	O  opt.Option[int]
	U  und.Und[string]
	E  elastic.Elastic[int]
	Ei either.Either[int, string]
	Ue either.UntaggedEither[int, string]
	R  either.Result[int]
	T  tuple.Tuple2[int, string]
	N  sql.Null[int]
	S  sql.NullString
	H  unique.Handle[string]
	P  *opt.Option[Inner]
}

type Inner struct {
	X int ` + "`json:\"x\"`" + `
}
`

type holdersInner struct{ X int }

type holders struct {
	O  opt.Option[int]
	U  und.Und[string]
	E  elastic.Elastic[int]
	Ei either.Either[int, string]
	Ue either.UntaggedEither[int, string]
	R  either.Result[int]
	T  tuple.Tuple2[int, string]
	N  sql.Null[int]
	S  sql.NullString
	H  unique.Handle[string]
	P  *opt.Option[holdersInner]
}

func TestHoldersTree(t *testing.T) {
	texts, warnings, err := gen(t, map[string]string{"p/p.go": holdersSrc}, nil)
	assert.NilError(t, err)
	assert.Equal(t, len(warnings), 0, "%v", warnings)
	golden.Assert(t, texts["holders"], "holders.golden")
}

// TestHoldersReachExecutes runs the template expression each support gives
// for each value against a real value, an index standing for i.
func TestHoldersReachExecutes(t *testing.T) {
	pkgs := base.SourcePackages(gentest.Load(t, map[string]string{"p/p.go": holdersSrc}))
	st := pkgs[0].Types.Scope().Lookup("Holders").Type().Underlying().(*types.Struct)
	w := walker{reg: genext.DefaultRegistry()}

	inner := opt.Some(holdersInner{X: 9})
	v := holders{
		O:  opt.Some(1),
		U:  und.Defined("u"),
		E:  elastic.FromOptions(opt.Some(3)),
		Ei: either.Left[int, string](4),
		Ue: either.UntaggedEither[int, string]{Either: either.Right[int]("ue")},
		R:  either.Err[int](errors.New("bad")),
		T:  tuple.Tuple2[int, string]{V1: 5, V2: "t"},
		N:  sql.Null[int]{V: 6, Valid: true},
		S:  sql.NullString{String: "s", Valid: true},
		H:  unique.Make("h"),
		P:  &inner,
	}

	want := map[string]string{
		"O t": "1", "U t": "u", "E t": "{true 3}",
		"Ei l": "4", "Ue r": "ue", "R err": "bad",
		"T v1": "5", "T v2": "t", "N t": "6", "S t": "s", "H t": "h",
	}

	for f := range st.Fields() {
		ft := deref(f.Type())

		for _, role := range genext.Roles(w.reg, ft) {
			access, note, _, ok := w.reach(ft, role, "."+f.Name())
			assert.Assert(t, ok, "%s %s", f.Name(), role)
			assert.Assert(t, note != "", "%s %s", f.Name(), role)

			name := f.Name() + " " + role

			w, ok := want[name]
			if !ok {
				continue
			}

			tmpl, err := template.New("").
				Parse("{{ " + strings.ReplaceAll(access, " i", " 0") + " }}")
			assert.NilError(t, err, name)

			var b strings.Builder
			assert.NilError(t, tmpl.Execute(&b, v), name)
			assert.Equal(t, b.String(), w, name)
		}
	}

	// A pointer to a holder walks through the pointer.
	tmpl := template.Must(template.New("").Parse("{{ .P.Value.X }}"))

	var b strings.Builder
	assert.NilError(t, tmpl.Execute(&b, v))
	assert.Equal(t, b.String(), "9")
}
