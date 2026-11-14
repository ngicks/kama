package genhelper_test

import (
	"errors"
	"fmt"
	"go/format"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
)

func TestExpr(t *testing.T) {
	errGen := errors.New("gen")

	type testCase struct {
		name    string
		setup   func(fn *genhelper.Scope)
		format  string
		args    []any
		want    string
		wantErr error
	}
	for _, tc := range []testCase{
		{
			name:   "Ref spells the declared name",
			setup:  func(fn *genhelper.Scope) { fn.Declare("v") },
			format: "len(%s)",
			args:   []any{genhelper.Ref("v")},
			want:   "len(v2)",
		},
		{
			name:    "Ref to an undeclared role",
			format:  "%s",
			args:    []any{genhelper.Ref("v")},
			wantErr: genhelper.ErrUndeclared,
		},
		{
			name:   "Decl declares a fresh name",
			format: "%s := 1",
			args:   []any{genhelper.Decl("v")},
			want:   "v2 := 1",
		},
		{
			name:   "Decl before Ref of the same role",
			format: "%s := %s",
			args:   []any{genhelper.Decl("x"), genhelper.Ref("x")},
			want:   "x := x",
		},
		{
			name:    "Ref before Decl of the same role",
			format:  "%s := %s",
			args:    []any{genhelper.Ref("x"), genhelper.Decl("x")},
			wantErr: genhelper.ErrUndeclared,
		},
		{
			name:   "an indexed verb reuses one rendering",
			format: "%[1]s = %[1]s",
			args:   []any{genhelper.Decl("x")},
			want:   "x = x",
		},
		{
			name:   "Use spells a pending name",
			format: "%s.X",
			args:   []any{genhelper.Use("v")},
			want:   "v2.X",
		},
		{
			name:   "Shadow takes exactly the role",
			format: "%s := 1",
			args:   []any{genhelper.Shadow("v")},
			want:   "v := 1",
		},
		{
			name:    "Shadow of a name the scope holds",
			setup:   func(fn *genhelper.Scope) { fn.Declare("x") },
			format:  "%s",
			args:    []any{genhelper.Shadow("x")},
			wantErr: genhelper.ErrRedeclared,
		},
		{
			name:   "Gen and func arguments render",
			setup:  func(fn *genhelper.Scope) { fn.Declare("x") },
			format: "%s(%s)",
			args: []any{
				genhelper.Gen[ctx](func(ctx) (string, error) { return "f", nil }),
				func(c ctx) (string, error) {
					name, _ := c.Scope().Lookup("x")
					return name, nil
				},
			},
			want: "f(x)",
		},
		{
			name:   "Name spells the declared name",
			setup:  func(fn *genhelper.Scope) { fn.Declare("x") },
			format: "%s",
			args:   []any{genhelper.Name[ctx]("x")},
			want:   "x",
		},
		{
			name:    "Name of an undeclared role",
			format:  "%s",
			args:    []any{genhelper.Name[ctx]("x")},
			wantErr: genhelper.ErrUndeclared,
		},
		{
			name:   "a percent sign in an argument is not a verb",
			format: "%s %d",
			args: []any{
				"100%d",
				3,
			},
			want: "100%d 3",
		},
		{
			name:   "a percent sign in a Gen argument is not a verb",
			format: "x = %s",
			args: []any{
				genhelper.Gen[ctx](func(ctx) (string, error) { return "%s", nil }),
			},
			want: "x = %s",
		},
		{
			name:   "Gen error",
			format: "%s",
			args: []any{
				genhelper.Gen[ctx](func(ctx) (string, error) { return "", errGen }),
			},
			wantErr: errGen,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := genhelper.NewScope()
			root.Reserve("v")
			fn := root.Push()
			if tc.setup != nil {
				tc.setup(fn)
			}

			got, err := genhelper.Expr[ctx](tc.format, tc.args...)(
				genhelper.NewBaseContext(fn, nil),
			)
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
				return
			}
			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}
}

func TestExprScope(t *testing.T) {
	t.Run("Decl declares in the scope of the context", func(t *testing.T) {
		fn := genhelper.NewScope().Push()
		_, err := genhelper.Expr[ctx]("%s", genhelper.Decl("v"))(genhelper.NewBaseContext(fn, nil))
		assert.NilError(t, err)

		name, ok := fn.Lookup("v")
		assert.Assert(t, ok)
		assert.Equal(t, name, "v")
		assert.Equal(t, fn.Declare("v"), "v2")
	})

	t.Run("a later Decl claims the name of Use", func(t *testing.T) {
		fn := genhelper.NewScope().Push()
		c := genhelper.NewBaseContext(fn, nil)

		used, err := genhelper.Expr[ctx]("%s", genhelper.Use("e"))(c)
		assert.NilError(t, err)
		declared, err := genhelper.Expr[ctx]("%s", genhelper.Decl("e"))(c)
		assert.NilError(t, err)
		assert.Equal(t, used, declared)
		assert.DeepEqual(t, fn.Unbound(), []string(nil))
	})
}

func TestStmtLine(t *testing.T) {
	type testCase struct {
		name    string
		step    genhelper.Step[ctx]
		want    string
		wantErr error
	}
	for _, tc := range []testCase{
		{
			name: "Stmt appends a line",
			step: genhelper.Stmt[ctx]("%s := %d", genhelper.Decl("n"), 1),
			want: "// head\nn := 1\n",
		},
		{
			name: "Stmt keeps a percent sign of an argument",
			step: genhelper.Stmt[ctx]("%s", "fmt.Sprintf(\"%d\", 1)"),
			want: "// head\nfmt.Sprintf(\"%d\", 1)\n",
		},
		{
			name: "Line appends a line",
			step: genhelper.Line(genhelper.Expr[ctx]("return %s", "100%")),
			want: "// head\nreturn 100%\n",
		},
		{
			name:    "Stmt error",
			step:    genhelper.Stmt[ctx]("%s", genhelper.Ref("x")),
			wantErr: genhelper.ErrUndeclared,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, b, err := tc.step.Render(
				genhelper.NewBaseContext(genhelper.NewScope(), nil),
				[]byte("// head\n"),
			)
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
				assert.Assert(t, b == nil)
				return
			}
			assert.NilError(t, err)
			assert.Equal(t, string(b), tc.want)
		})
	}
}

func TestFunc(t *testing.T) {
	sig := func(name string) genhelper.Gen[ctx] {
		return genhelper.Expr[ctx]("func %s(%s int) int", name, genhelper.Decl("v"))
	}
	retV := genhelper.Stmt[ctx]("return %s", genhelper.Ref("v"))

	type testCase struct {
		name    string
		setup   func(root *genhelper.Scope)
		step    genhelper.Step[ctx]
		want    string
		wantErr error
		wantMsg string
	}
	for _, tc := range []testCase{
		{
			name: "the body refers to a parameter",
			step: genhelper.Func(sig("f"), retV),
			want: "func f(v int) int {\nreturn v\n}\n",
		},
		{
			name: "an empty body",
			step: genhelper.Func(genhelper.Expr[ctx]("func f()")),
			want: "func f() {\n}\n",
		},
		{
			name: "a parameter is not visible after the function",
			step: genhelper.Steps[ctx]{
				genhelper.Func(sig("f"), retV),
				genhelper.Stmt[ctx]("_ = %s", genhelper.Ref("v")),
			},
			wantErr: genhelper.ErrUndeclared,
			wantMsg: "genhelper: step 1: genhelper: undeclared in this scope: v",
		},
		{
			name: "sibling functions reuse names",
			step: genhelper.Steps[ctx]{
				genhelper.Func(sig("f"), retV),
				genhelper.Func(sig("g"), retV),
			},
			want: "func f(v int) int {\nreturn v\n}\nfunc g(v int) int {\nreturn v\n}\n",
		},
		{
			name:  "a name reserved outside gives a fresh parameter",
			setup: func(root *genhelper.Scope) { root.Reserve("v") },
			step:  genhelper.Func(sig("f"), retV),
			want:  "func f(v2 int) int {\nreturn v2\n}\n",
		},
		{
			name: "the body shares the scope of the parameters",
			step: genhelper.Func(
				sig("f"),
				// The Ref renders before the Decl of the same role.
				genhelper.Stmt[ctx](
					"%[2]s, %[3]s := %[1]s, 1",
					genhelper.Ref("v"), genhelper.Decl("w"), genhelper.Decl("v"),
				),
				genhelper.Stmt[ctx]("return %s + %s", genhelper.Ref("w"), genhelper.Ref("v")),
			),
			want: "func f(v int) int {\nw, v2 := v, 1\nreturn w + v2\n}\n",
		},
		{
			name: "a signature error",
			step: genhelper.Func(
				genhelper.Expr[ctx]("func f(%s)", genhelper.Ref("x")),
				retV,
			),
			wantErr: genhelper.ErrUndeclared,
			wantMsg: "genhelper: undeclared in this scope: x",
		},
		{
			name: "a body error names its step",
			step: genhelper.Steps[ctx]{
				genhelper.Func(sig("f"), retV, genhelper.Stmt[ctx]("return %s", genhelper.Ref("x"))),
			},
			wantErr: genhelper.ErrUndeclared,
			wantMsg: "genhelper: step 0.1: genhelper: undeclared in this scope: x",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := genhelper.NewScope()
			if tc.setup != nil {
				tc.setup(root)
			}

			c, b, err := tc.step.Render(genhelper.NewBaseContext(root, nil), nil)
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
				assert.Error(t, err, tc.wantMsg)
				return
			}
			assert.NilError(t, err)
			assert.Equal(t, string(b), tc.want)
			assert.Equal(t, c.Scope(), root)
		})
	}
}

func TestFuncStatements(t *testing.T) {
	step := genhelper.Func(
		genhelper.Expr[ctx]("func (%s Set) Keys() []string", genhelper.Decl("s")),
		genhelper.Stmt[ctx]("var %s []string", genhelper.Decl("out")),
		genhelper.ForRange("k", "v", genhelper.Name[ctx]("s"),
			genhelper.If(
				genhelper.Expr[ctx]("%s", genhelper.Ref("v")),
				genhelper.Stmt[ctx](
					"%[1]s = append(%[1]s, %[2]s)",
					genhelper.Ref("out"),
					genhelper.Ref("k"),
				),
				nil,
			),
		),
		genhelper.Stmt[ctx]("return %s", genhelper.Ref("out")),
	)

	root := genhelper.NewScope()
	root.Reserve("k")
	_, b, err := step.Render(genhelper.NewBaseContext(root, nil), nil)
	assert.NilError(t, err)
	b, err = format.Source(b)
	assert.NilError(t, err)
	assert.Equal(t, string(b), `func (s Set) Keys() []string {
	var out []string
	for k2, v := range s {
		if v {
			out = append(out, k2)
		}
	}
	return out
}
`)
}

type pathError struct{}

func (pathError) Error() string { return "path" }

func TestStepsErrorPath(t *testing.T) {
	ok := genhelper.Stmt[ctx]("ok")
	fail := genhelper.FuncStep[ctx](func(c ctx, _ []byte) (ctx, []byte, error) {
		return c, nil, pathError{}
	})

	type testCase struct {
		name string
		step genhelper.Step[ctx]
		want string
	}
	for _, tc := range []testCase{
		{
			name: "one level",
			step: genhelper.Steps[ctx]{ok, fail},
			want: "genhelper: step 1: path",
		},
		{
			name: "nested Steps extend the path",
			step: genhelper.Steps[ctx]{
				ok,
				genhelper.Steps[ctx]{genhelper.Steps[ctx]{ok, ok, fail}},
			},
			want: "genhelper: step 1.0.2: path",
		},
		{
			name: "statement steps pass the path on",
			step: genhelper.Steps[ctx]{
				ok,
				genhelper.ForRange("k", "", genhelper.Expr[ctx]("xs"), genhelper.Steps[ctx]{ok, fail}),
			},
			want: "genhelper: step 1.1: path",
		},
		{
			name: "a step wrapping the error keeps its message",
			step: genhelper.Steps[ctx]{
				genhelper.FuncStep[ctx](func(c ctx, b []byte) (ctx, []byte, error) {
					c, b, err := genhelper.Steps[ctx]{fail}.Render(c, b)
					if err != nil {
						return c, nil, fmt.Errorf("wrap: %w", err)
					}
					return c, b, nil
				}),
			},
			want: "genhelper: step 0: wrap: genhelper: step 0: path",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := tc.step.Render(genhelper.NewBaseContext(genhelper.NewScope(), nil), nil)
			assert.Error(t, err, tc.want)
			assert.ErrorIs(t, err, pathError{})
			var pe pathError
			assert.Assert(t, errors.As(err, &pe))
		})
	}
}
