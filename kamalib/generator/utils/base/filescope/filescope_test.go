package filescope_test

import (
	"bytes"
	"go/types"
	"strings"
	"testing"

	"github.com/ngicks/kama/kamalib/pkg/importdecl/dstimport"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base/filescope"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
)

func TestFileScope(t *testing.T) {
	pkgs := gentest.Load(t, map[string]string{
		"a.go": "package x\n\nimport \"time\"\n\n" +
			"type A struct {\n\tT time.Time\n\tB *B\n}\n\ntype B struct{}\n\nvar strings int\n",
	})
	pkg := pkgs[0]

	s, err := filescope.New(pkg, []string{"fmt"}, []string{"fmt", "strings"})
	assert.NilError(t, err)

	st := pkg.Types.Scope().Lookup("A").Type().Underlying().(*types.Struct)

	// A type of another package is qualified, and a type of the package of
	// the file is not.
	got, err := s.TypeString(st.Field(0).Type())
	assert.NilError(t, err)
	assert.Equal(t, got, "time.Time")

	got, err = s.TypeString(st.Field(1).Type())
	assert.NilError(t, err)
	assert.Equal(t, got, "*B")

	// An import name avoids the reserved identifiers and the package-level
	// identifiers of the package.
	fmtIdent, ok := s.Ident("fmt")
	assert.Assert(t, ok)
	assert.Assert(t, fmtIdent != "fmt", fmtIdent)

	stringsIdent, ok := s.Ident("strings")
	assert.Assert(t, ok)
	assert.Assert(t, stringsIdent != "strings", stringsIdent)

	_, ok = s.Ident("example.invalid/unknown")
	assert.Assert(t, !ok)

	assert.Equal(t, s.Position(st.Field(0).Pos()).Line, 6)

	// Only the packages used are imported.
	imports := func(s *filescope.Scope) string {
		t.Helper()

		f := &dst.File{Name: dst.NewIdent("x")}
		dstimport.AddMissing(s.Imports, f)

		var buf bytes.Buffer
		assert.NilError(t, decorator.Fprint(&buf, f))

		return buf.String()
	}

	src := imports(s)
	for _, path := range []string{`"time"`, `"fmt"`, `"strings"`} {
		assert.Assert(t, strings.Contains(src, path), src)
	}

	// A fresh scope knows the same packages and uses none of them.
	fresh, err := s.Fresh()
	assert.NilError(t, err)

	_, ok = fresh.Ident("fmt")
	assert.Assert(t, ok)

	src = imports(fresh)
	assert.Assert(t, strings.Contains(src, `"fmt"`), src)
	assert.Assert(t, !strings.Contains(src, `"time"`), src)
}

func TestFileScopeFuncContext(t *testing.T) {
	pkgs := gentest.Load(t, map[string]string{"a.go": "package x\n\nvar v int\n"})

	s, err := filescope.New(pkgs[0], nil, nil)
	assert.NilError(t, err)

	// A local of a generated function avoids the package-level identifiers
	// and the type parameters of the function.
	c := s.FuncContext([]string{"T"})
	for _, taken := range []string{"v", "T", "len"} {
		assert.Assert(t, c.Scope().Declare(taken) != taken, taken)
	}
}
