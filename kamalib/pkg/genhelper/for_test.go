package genhelper_test

import (
	"errors"
	"go/format"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
)

func TestFor(t *testing.T) {
	type ctx = genhelper.BaseContext
	var g genhelper.Lang[ctx]
	body := g.Stmt("use(%s)", g.Name("i"))

	for _, tc := range []struct {
		name string
		step genhelper.Step[ctx]
		want string
	}{
		{
			name: "three clauses",
			step: g.For(
				g.Expr("%s := 0", genhelper.Decl("i")),
				g.Expr("%s < n", g.Name("i")),
				g.Expr("%s++", g.Name("i")),
				body,
			),
			want: "for i2 := 0; i2 < n; i2++ {\n\tuse(i2)\n}\n",
		},
		{
			name: "condition only",
			step: g.Steps(
				g.Stmt("%s := 0", genhelper.Decl("i")),
				g.For(nil, g.Expr("%s < n", g.Name("i")), nil, body),
			),
			want: "i2 := 0\nfor i2 < n {\n\tuse(i2)\n}\n",
		},
		{
			name: "bare",
			step: g.For(nil, nil, nil, g.Stmt("break")),
			want: "for {\n\tbreak\n}\n",
		},
		{
			name: "init without condition and post",
			step: g.For(g.Expr("%s := 0", genhelper.Decl("i")), nil, nil, body),
			want: "for i2 := 0; ; {\n\tuse(i2)\n}\n",
		},
		{
			name: "nil body",
			step: g.For(nil, g.Expr("next()"), nil, nil),
			want: "for next() {\n}\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := genhelper.NewScope()
			// The destination package declares i.
			s.Reserve("i")
			out, err := g.Render(genhelper.NewBaseContext(s.Push(), nil), tc.step)
			assert.NilError(t, err)
			b, err := format.Source([]byte(out))
			assert.NilError(t, err)
			assert.Equal(t, string(b), tc.want)
		})
	}
}

func TestForScope(t *testing.T) {
	type ctx = genhelper.BaseContext
	var g genhelper.Lang[ctx]

	// The variable init declares is gone after the loop, so a loop beside
	// it reuses the name.
	loop := g.For(
		g.Expr("%s := 0", genhelper.Decl("i")),
		g.Expr("%s < 3", g.Name("i")),
		g.Expr("%s++", g.Name("i")),
		nil,
	)
	out, err := g.Render(genhelper.NewBaseContext(genhelper.NewScope(), nil), loop, loop)
	assert.NilError(t, err)
	assert.Equal(t, out, "for i := 0; i < 3; i++ {\n}\nfor i := 0; i < 3; i++ {\n}\n")

	_, err = g.Render(
		genhelper.NewBaseContext(genhelper.NewScope(), nil),
		loop,
		g.Stmt("use(%s)", g.Name("i")),
	)
	assert.ErrorIs(t, err, genhelper.ErrUndeclared)

	boom := errors.New("boom")
	_, err = g.Render(
		genhelper.NewBaseContext(genhelper.NewScope(), nil),
		g.For(nil, g.FailGen(boom), nil, nil),
	)
	assert.ErrorIs(t, err, boom)
}
