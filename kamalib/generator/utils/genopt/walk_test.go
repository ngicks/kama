package genopt

import (
	"fmt"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/packages"
	"gotest.tools/v3/assert"
)

const fixturePath = "github.com/ngicks/kama/kamalib/generator/utils/genopt/internal/fixture"

func TestWalk(t *testing.T) {
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedCompiledGoFiles | packages.NeedSyntax,
	}, "./internal/fixture")
	assert.NilError(t, err)
	assert.Equal(t, len(pkgs), 1)
	assert.Equal(t, len(pkgs[0].Errors), 0, "%v", pkgs[0].Errors)

	ws, err := walk(pkgs, "kama")
	assert.NilError(t, err)
	assert.Equal(t, len(ws), 1)

	// Each line spells <file>:<line>:<column> <target> //kama:<directive> <args>.
	got := make([]string, len(ws[0].lines))
	for i, l := range ws[0].lines {
		got[i] = fmt.Sprintf(
			"%s:%d:%d %s //kama:%s %s",
			filepath.Base(l.pos.Filename), l.pos.Line, l.pos.Column,
			spellTarget(l.target), l.directive, l.args,
		)
	}

	assert.DeepEqual(t, got, []string{
		"a.go:3:1 file a.go //kama:partial package",
		"a.go:11:1 file a.go //kama:partial file",
		"a.go:17:1 type Single //kama:partial ",
		"a.go:21:2 field Single.A //kama:partial:merge append",
		`a.go:23:11 field Single.B //kama:partial:flag name=addr short=a usage="listen address"`,
		"a.go:26:2 field Single.C //kama:partial:x ",
		"a.go:26:2 field Single.D //kama:partial:x ",
		"a.go:28:16 field Single.Buffer //kama:partial:embedded ",
		// The doc comment of a declaration with several specs is the type
		// group of its types.
		"a.go:36:1 group //kama:partial ignore",
		"a.go:38:2 type G1 //kama:partial grouped",
		"a.go:46:2 field WithGeneric.Generic //kama:partial:gen ",
		"b.go:5:1 file b.go //kama:partial file",
		"b.go:6:1 file b.go //kama:partial:other x=1",
		// A parenthesized struct type keeps its field directives.
		"c.go:4:2 field Paren.X //kama:partial:paren ",
		// Line comments of declarations are not free-floating; the group
		// on the line after one is.
		"c.go:9:1 file c.go //kama:partial next line",
	})
}

func spellTarget(t Target) string {
	switch t.Kind {
	case TargetFreeFloating:
		return "file " + filepath.Base(t.File)
	case TargetTypeGroup:
		return "group"
	case TargetType:
		return "type " + t.Type
	case TargetField:
		return "field " + t.Type + "." + t.Field
	}

	return "unknown"
}
