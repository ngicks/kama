package genhelper_test

import (
	"errors"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
)

func TestLang(t *testing.T) {
	var l genhelper.Lang[ctx]

	errBoom := errors.New("boom")

	type (
		decl = genhelper.Decl
		ref  = genhelper.Ref
	)

	// fnCtx returns a context positioned in a function scope that declares v,
	// fresh for each render because a Decl changes the scope.
	fnCtx := func() ctx {
		root := genhelper.NewScope()
		root.ReserveUniverse()
		fn := root.Push()
		fn.Declare("v")

		return genhelper.NewBaseContext(fn, nil)
	}
	paren := func(_ ctx, inner string) (string, error) { return "(" + inner + ")", nil }
	lit := func(s string) func(ctx) (string, error) {
		return func(ctx) (string, error) { return s, nil }
	}

	type testCase struct {
		name     string
		method   func() step
		function func() step
		wantErr  error
	}
	for _, tc := range []testCase{
		{
			name:     "Expr",
			method:   func() step { return l.Line(l.Expr("%s := %s", decl("x"), ref("v"))) },
			function: func() step { return genhelper.Line(expr("%s := %s", decl("x"), ref("v"))) },
		},
		{
			name:     "Stmt",
			method:   func() step { return l.Stmt("%s := len(%s)", decl("v"), ref("v")) },
			function: func() step { return stmt("%s := len(%s)", decl("v"), ref("v")) },
		},
		{
			name:     "Name",
			method:   func() step { return l.Line(l.Name("v")) },
			function: func() step { return genhelper.Line(genhelper.Name[ctx]("v")) },
		},
		{
			name:     "Name of an undeclared role",
			method:   func() step { return l.Line(l.Name("x")) },
			function: func() step { return genhelper.Line(genhelper.Name[ctx]("x")) },
			wantErr:  genhelper.ErrUndeclared,
		},
		{
			name:   "Join",
			method: func() step { return l.Line(l.Join(", ", l.Name("v"), l.Expr("1"))) },
			function: func() step {
				return genhelper.Line(genhelper.Join(", ", genhelper.Name[ctx]("v"), expr("1")))
			},
		},
		{
			name:     "Join without parts",
			method:   func() step { return l.Line(l.Join(", ")) },
			function: func() step { return genhelper.Line(genhelper.Join[ctx](", ")) },
		},
		{
			name:   "Comment",
			method: func() step { return l.Line(l.Comment(l.Expr("doc of %s", ref("v")), l.Expr(""))) },
			function: func() step {
				return genhelper.Line(genhelper.Comment(expr("doc of %s", ref("v")), expr("")))
			},
		},
		{
			name:     "Comment without parts",
			method:   func() step { return l.Line(l.Comment()) },
			function: func() step { return genhelper.Line(genhelper.Comment[ctx]()) },
		},
		{
			name:     "Fail",
			method:   func() step { return l.Fail(errBoom) },
			function: func() step { return genhelper.Fail[ctx](errBoom) },
			wantErr:  errBoom,
		},
		{
			name:     "FailGen",
			method:   func() step { return l.Line(l.FailGen(errBoom)) },
			function: func() step { return genhelper.Line(genhelper.FailGen[ctx](errBoom)) },
			wantErr:  errBoom,
		},
		{
			name: "DocFunc",
			method: func() step {
				return l.DocFunc(
					l.Comment(l.Expr("F returns %s.", ref("x"))),
					l.Expr("func F(%s int) int", decl("x")),
					l.Stmt("return %s", ref("x")),
				)
			},
			function: func() step {
				return genhelper.DocFunc(
					genhelper.Comment(expr("F returns %s.", ref("x"))),
					expr("func F(%s int) int", decl("x")),
					stmt("return %s", ref("x")),
				)
			},
		},
		{
			name: "Func, ForRange, If and Switch",
			method: func() step {
				return l.Func(
					l.Expr("func F(%s []int)", decl("xs")),
					l.ForRange("i", "v", l.Name("xs"), l.Steps(
						l.If(l.Expr("%s == 0", ref("i")), l.Stmt("continue"), l.Stmt("break")),
						l.Switch(l.Name("v"),
							l.Case(l.Expr("0"), l.Stmt("return")),
							l.Default(l.Stmt("println(%s)", ref("v"))),
						),
					)),
				)
			},
			function: func() step {
				return genhelper.Func(
					expr("func F(%s []int)", decl("xs")),
					genhelper.ForRange("i", "v", genhelper.Name[ctx]("xs"), genhelper.Steps[ctx]{
						genhelper.If(expr("%s == 0", ref("i")), stmt("continue"), stmt("break")),
						genhelper.Switch(genhelper.Name[ctx]("v"),
							genhelper.Case[ctx]{List: expr("0"), Body: stmt("return")},
							genhelper.Case[ctx]{Body: stmt("println(%s)", ref("v"))},
						),
					}),
				)
			},
		},
		{
			name:   "Append and Wrap",
			method: func() step { return l.Steps(l.Append(lit("x")), l.Wrap(paren)) },
			function: func() step {
				return genhelper.Steps[ctx]{genhelper.Append(lit("x")), genhelper.Wrap(paren)}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := l.Render(fnCtx(), tc.method())

			want, wantErr := genhelper.Render(fnCtx(), tc.function())
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
				assert.ErrorIs(t, wantErr, tc.wantErr)
				assert.Equal(t, err.Error(), wantErr.Error())

				return
			}

			assert.NilError(t, err)
			assert.NilError(t, wantErr)
			assert.Assert(t, got != "")
			assert.Equal(t, got, want)
		})
	}

	t.Run("DocComment ends with the marker of the generator", func(t *testing.T) {
		got, err := l.Render(
			fnCtx(),
			l.Line(l.DocComment("//x:generated x-gen", "Doc of %s.", ref("v"))),
		)
		assert.NilError(t, err)
		assert.Equal(t, got, "// Doc of v.\n//\n//x:generated x-gen\n")
	})

	t.Run("FailGen returns err as it is", func(t *testing.T) {
		_, err := l.FailGen(errBoom)(fnCtx())
		assert.Equal(t, err, errBoom)
	})
}
