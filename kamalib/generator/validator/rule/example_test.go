package rule_test

import (
	"fmt"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/rule"
	"github.com/ngicks/kama/kamalib/pkg/genhelper"
)

// multiple is the validator the package doc writes. Its code must stay
// textually the same as the doc example.
type multiple struct{}

func (multiple) Name() string { return "multiple" }

type multipleOptions struct{ N int64 }

func (multiple) Parse(arg string, _ rule.Resolver) (rule.Rule, error) {
	n, err := strconv.ParseInt(arg, 10, 64)
	if err != nil || n <= 0 {
		return nil, fmt.Errorf("%w: want a positive integer", rule.ErrArgs)
	}

	return multipleOptions{N: n}, nil
}

func (multipleOptions) Check(t types.Type) error {
	if b, ok := t.Underlying().(*types.Basic); !ok || b.Info()&types.IsInteger == 0 {
		return fmt.Errorf("%s is not an integer type", t)
	}

	return nil
}

var lang genhelper.Lang[rule.Context]

func (o multipleOptions) Emit(e rule.Emitter, _ types.Type, v rule.Gen) (rule.Gen, error) {
	e.Declare("kamaValidateMultiple", func(imp rule.Importer) rule.Step {
		return lang.Func(
			lang.Expr("func kamaValidateMultiple(%s, %s int64) error",
				genhelper.Shadow("got"), genhelper.Shadow("n")),
			lang.If(
				lang.Expr("%s%%%s != 0", genhelper.Ref("got"), genhelper.Ref("n")),
				lang.Stmt(`return %s.New("want a multiple")`, imp.Import("errors")),
				nil,
			),
			lang.Stmt("return nil"),
		)
	})

	return lang.Expr("kamaValidateMultiple(int64(%s), %d)", v, o.N), nil
}

// TestExampleMatchesDoc keeps the doc example compiled: the declarations of
// the example in the package doc appear verbatim in this file.
func TestExampleMatchesDoc(t *testing.T) {
	f, err := parser.ParseFile(
		token.NewFileSet(), "doc.go", nil, parser.PackageClauseOnly|parser.ParseComments,
	)
	assert.NilError(t, err)

	doc := f.Doc.Text()
	start := strings.Index(doc, "\ttype multiple struct{}\n")
	end := strings.Index(doc, "\n\treg := ")
	assert.Assert(t, start >= 0 && end > start, "no example in\n%s", doc)

	example := strings.ReplaceAll(doc[start+1:end], "\n\t", "\n")

	src, err := os.ReadFile("example_test.go")
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(string(src), example), "want\n%s", example)
}

func TestExampleParse(t *testing.T) {
	r, err := multiple{}.Parse("3", nil)
	assert.NilError(t, err)
	assert.Equal(t, r, rule.Rule(multipleOptions{N: 3}))

	for _, arg := range []string{"", "0", "-3", "1.5", "three"} {
		_, err := multiple{}.Parse(arg, nil)
		assert.ErrorIs(t, err, rule.ErrArgs, "arg %q", arg)
		assert.ErrorContains(t, err, "want a positive integer", "arg %q", arg)
	}
}

func TestExampleCheck(t *testing.T) {
	named := types.NewNamed(
		types.NewTypeName(0, nil, "Count", nil),
		types.Typ[types.Uint8],
		nil,
	)

	for _, typ := range []types.Type{types.Typ[types.Int], types.Typ[types.Int64], named} {
		assert.NilError(t, multipleOptions{N: 3}.Check(typ), "type %s", typ)
	}

	for _, typ := range []types.Type{
		types.Typ[types.String],
		types.Typ[types.Float64],
		types.NewPointer(types.Typ[types.Int]),
	} {
		err := multipleOptions{N: 3}.Check(typ)
		assert.ErrorContains(t, err, typ.String()+" is not an integer type")
	}
}

func TestExampleRegister(t *testing.T) {
	reg := rule.DefaultRegistry().Clone()
	reg.Register(multiple{})

	assert.Assert(t, slices.Contains(reg.Names(), "multiple"))
	assert.Assert(t, slices.Contains(reg.Names(), "length"))
	assert.Assert(t, !slices.Contains(rule.DefaultRegistry().Names(), "multiple"))

	v, ok := reg.Lookup("multiple")
	assert.Assert(t, ok)
	assert.Equal(t, v, rule.Validator(multiple{}))
}
