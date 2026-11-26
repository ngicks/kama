package expr

import (
	"fmt"
	"strings"
	"testing"

	"go.starlark.net/syntax"
	"gotest.tools/v3/assert"
)

// These tests pin the behaviour of go.starlark.net/syntax that Parse
// builds on.

func parseStarlark(t *testing.T, src string) (syntax.Expr, error) {
	t.Helper()
	return (&syntax.FileOptions{}).ParseExpr("", src, 0)
}

// shape spells the node types of e, children in parentheses.
func shape(e syntax.Expr) string {
	var b strings.Builder

	var visit func(syntax.Expr)

	visit = func(e syntax.Expr) {
		children := func(es ...syntax.Expr) {
			b.WriteString("(")

			for i, c := range es {
				if i > 0 {
					b.WriteString(" ")
				}

				visit(c)
			}

			b.WriteString(")")
		}

		switch x := e.(type) {
		case *syntax.Ident:
			b.WriteString(x.Name)
		case *syntax.Literal:
			fmt.Fprintf(&b, "%s:%v", x.Raw, x.Value)
		case *syntax.TupleExpr:
			b.WriteString("Tuple")
			children(x.List...)
		case *syntax.ParenExpr:
			b.WriteString("Paren")
			children(x.X)
		case *syntax.CallExpr:
			b.WriteString("Call")
			children(append([]syntax.Expr{x.Fn}, x.Args...)...)
		case *syntax.BinaryExpr:
			fmt.Fprintf(&b, "%s", x.Op)
			children(x.X, x.Y)
		case *syntax.UnaryExpr:
			fmt.Fprintf(&b, "%s", x.Op)
			children(x.X)
		case *syntax.CondExpr:
			b.WriteString("Cond")
			children(x.True, x.Cond, x.False)
		default:
			fmt.Fprintf(&b, "%T", e)
		}
	}

	visit(e)

	return b.String()
}

func TestStarlarkShapes(t *testing.T) {
	cases := []struct {
		src, shape string
	}{
		// A top-level comma gives a tuple without parentheses.
		{"a, b", "Tuple(a b)"},
		// Parentheses survive as a node.
		{"(a)", "Paren(a)"},
		{"(a, b)", "Paren(Tuple(a b))"},
		// A raw string keeps the pattern unchanged.
		{`r"^[a-z]+$"`, `r"^[a-z]+$":^[a-z]+$`},
		{`r'a"b\d'`, `r'a"b\d':a"b\d`},
		// A backslash keeps the quote after it inside a raw string.
		{`r"a\"b"`, `r"a\"b":a\"b`},
		// # outside a string starts a comment.
		{"a # comment", "a"},
		{`"a#b"`, `"a#b":a#b`},
		// A keyword arg is a binary = inside the call.
		{"f(a, k=1)", "Call(f a =(k 1:1))"},
		// The parser accepts a positional arg after a keyword arg and a
		// repeated keyword; Parse rejects both itself.
		{"f(k=1, a)", "Call(f =(k 1:1) a)"},
		{"f(k=1, k=2)", "Call(f =(k 1:1) =(k 2:2))"},
		// and and or nest to the left; parentheses keep their group.
		{"a and b and c", "and(and(a b) c)"},
		{"a and (b and c)", "and(a Paren(and(b c)))"},
		{"a or b and c", "or(a and(b c))"},
		{"not a", "not(a)"},
		{"not not a", "not(not(a))"},
		{"a if c else b", "Cond(a c b)"},
		{"a if b else c if d else e", "Cond(a b Cond(c d e))"},
		// The text wrapped as the args of a call takes keyword args.
		{
			"_(required, hook=off, length(min=1))",
			"Call(_ required =(hook off) Call(length =(min 1:1)))",
		},
		{"_(pre=f, post=g)", "Call(_ =(pre f) =(post g))"},
		{"_()", "Call(_)"},
		{"_(a,)", "Call(_ a)"},
		// The wrapping call can be closed early by the text.
		{"_(a) or _(b)", "or(Call(_ a) Call(_ b))"},
		{"_(a) (b)", "Call(Call(_ a) b)"},
		// Number literals keep their raw text.
		{
			"123456789012345678901234567890",
			"123456789012345678901234567890:123456789012345678901234567890",
		},
		{"0.1", "0.1:0.1"},
		{"1e-400", "1e-400:0"},
		{".5", ".5:0.5"},
		{"1.", "1.:1"},
		{"0x1F", "0x1F:31"},
		// A sign is a unary operator.
		{"-1", "-(1:1)"},
		{"+3", "+(3:3)"},
		// None, True and False are identifiers to the parser.
		{"None", "None"},
		{"True", "True"},
		{"None()", "Call(None)"},
		{"[1, None]", "*syntax.ListExpr"},
		{`{"a": 1}`, "*syntax.DictExpr"},
		{"assert", "assert"},
	}

	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			e, err := parseStarlark(t, c.src)
			assert.NilError(t, err)
			assert.Equal(t, shape(e), c.shape)
		})
	}
}

func TestStarlarkErrors(t *testing.T) {
	cases := []struct {
		src, err string
	}{
		// The position of an error of the parser may follow the token at
		// fault, as with = here.
		{"a if c", "1:3: conditional expression without else clause"},
		{"hook=off", "1:6: got '=' after expression, want EOF"},
		{"pre=f, post=g", "1:5: got '=' after expression, want EOF"},
		{"_(a # c)", "got end of file, want ')'"},
		{"1e400", "invalid float literal"},
		{"007", "obsolete form of octal literal"},
		{"1_000", "got identifier after expression"},
		{"`a`", "unexpected input character '`'"},
		{`"\q"`, `invalid escape sequence \q`},
		{`r"a\"`, "unexpected EOF in string"},
		{"\"a\nb\"", "unexpected newline in string"},
		{"f(a=b=c)", "got '=', want ','"},
		// Only an identifier stands before = in an arg.
		{"f(1=2)", "keyword argument must have form name=expr"},
		{`f("a"=1)`, "keyword argument must have form name=expr"},
	}

	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			_, err := parseStarlark(t, c.src)
			assert.ErrorContains(t, err, c.err)
		})
	}
}

// reservedWords are the keywords and the reserved words of Starlark. None of
// them parses as an identifier.
const reservedWords = `and break continue def elif else for if in lambda load not or
	pass return while as async await class del except finally from global import is
	nonlocal raise try with yield`

func TestStarlarkReservedWords(t *testing.T) {
	for w := range strings.FieldsSeq(reservedWords) {
		_, err := parseStarlark(t, w)
		assert.Assert(t, err != nil, w)
	}
}

func TestStarlarkColumns(t *testing.T) {
	// Columns count runes, and a tab counts one.
	e, err := parseStarlark(t, "_(\té, b)")
	assert.NilError(t, err)
	assert.Equal(t, e.(*syntax.CallExpr).Args[1].(*syntax.Ident).NamePos.Col, int32(7))
}
