package ext_test

import (
	"fmt"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"slices"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	_ "github.com/ngicks/kama/kamalib/generator/validator/ext/basic"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
	"github.com/ngicks/kama/kamalib/pkg/genhelper"
)

// multiple is the validator the package doc writes. Its code must stay
// textually the same as the doc example.
type multiple struct{}

func (multiple) Name() string { return "multiple" }

type multipleArgs struct {
	N int64 `json:"n"`
}

var multipleParams = ext.Params{Positional: []string{"n"}}

func (a multipleArgs) Call() (expr.Call, error) {
	return multipleParams.Call(multiple{}.Name(), a)
}

type multipleOptions struct{ N int64 }

func (multiple) Parse(c expr.Call, r ext.Resolver) (ext.Rule, error) {
	var args multipleArgs

	if _, err := multipleParams.Bind(r, c, &args); err != nil {
		return nil, err
	}

	if args.N <= 0 {
		return nil, fmt.Errorf("%w: want a positive integer", ext.ErrArgs)
	}

	return multipleOptions(args), nil
}

func (multipleOptions) Check(t types.Type) error {
	if b, ok := t.Underlying().(*types.Basic); !ok || b.Info()&types.IsInteger == 0 {
		return fmt.Errorf("%s is not an integer type", t)
	}

	return nil
}

var lang genhelper.Lang[ext.Context]

func (o multipleOptions) Emit(e ext.Emitter, _ types.Type, v ext.Gen) (ext.Gen, error) {
	e.Declare("kamaValidateMultiple", func(imp ext.Importer) ext.Step {
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
	parse := func(text string) (ext.Rule, error) {
		args, err := expr.Parse(text)
		assert.NilError(t, err)

		return multiple{}.Parse(args[0].Value.(expr.Call), ext.NewRegistry())
	}

	c, err := multipleArgs{N: 3}.Call()
	assert.NilError(t, err)
	assert.Equal(t, c.String(), "multiple(3)")

	r, err := parse("multiple(3)")
	assert.NilError(t, err)
	assert.Equal(t, r, ext.Rule(multipleOptions{N: 3}))

	for _, text := range []string{"multiple", "multiple(0)", "multiple(-3)"} {
		_, err := parse(text)
		assert.ErrorIs(t, err, ext.ErrArgs, text)
		assert.ErrorContains(t, err, "want a positive integer", text)
	}

	for _, text := range []string{"multiple(1.5)", `multiple("three")`, "multiple(1, 2)"} {
		_, err := parse(text)
		assert.ErrorIs(t, err, ext.ErrArgs, text)
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
	reg := ext.DefaultRegistry().Clone()
	reg.Register(multiple{})

	assert.Assert(t, slices.Contains(reg.Names(), "multiple"))
	assert.Assert(t, slices.Contains(reg.Names(), "length"))
	assert.Assert(t, !slices.Contains(ext.DefaultRegistry().Names(), "multiple"))

	v, ok := reg.Lookup("multiple")
	assert.Assert(t, ok)
	assert.Equal(t, v, ext.Validator(multiple{}))
}
