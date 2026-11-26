package expr

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"go.starlark.net/syntax"
)

// Parse parses text, the args of one directive, as comma separated items.
// Each item is a positional arg, an expression, or a keyword arg,
// name=expression. The package doc describes the grammar.
func Parse(text string) ([]Arg, error) {
	src := prefix + text + suffix

	e, err := (&syntax.FileOptions{}).ParseExpr("", src, 0)
	if err != nil {
		if se, ok := errors.AsType[syntax.Error](err); ok {
			return nil, &Error{Pos: position(text, se.Pos), Msg: se.Msg}
		}

		return nil, &Error{Pos: positionAt(text, 0), Msg: err.Error()}
	}

	p := parser{text: text}

	call, ok := e.(*syntax.CallExpr)
	if !ok || !isWrapper(call.Fn) {
		return nil, p.unbalanced(e)
	}

	return p.args(call)
}

// ErrSyntax is wrapped by every error of [Parse].
var ErrSyntax = errors.New("expr: syntax error")

// Error is an error of [Parse] located in the text of the directive. It
// wraps [ErrSyntax].
type Error struct {
	Pos Position
	Msg string
}

func (e *Error) Error() string {
	return fmt.Sprintf("expr: %d:%d: %s", e.Pos.Line, e.Pos.Column, e.Msg)
}

func (e *Error) Unwrap() error { return ErrSyntax }

// Position is a place in the text given to [Parse]. Offset counts bytes
// from 0, Line counts lines from 1, and Column counts the runes of the line
// from 1. A node built by hand holds the zero Position.
type Position struct {
	Offset int
	Line   int
	Column int
}

// Node is an expression of the tree [Parse] returns.
type Node interface {
	Pos() Position
	// String spells the node as Parse reads it back.
	String() string
	node()
}

// Arg is an item of the args of a directive or of a call. Name is empty for
// a positional arg.
type Arg struct {
	Start Position
	Name  string
	Value Node
}

// Call is a name, alone or followed by args in parentheses, as in required,
// length(min=1) or f(). Parens tells f() from f.
type Call struct {
	Start  Position
	Name   string
	Parens bool
	Args   []Arg
}

// And is a chain a and b and ... . A parenthesized chain stays one operand,
// so a and (b and c) holds an And as its second operand.
type And struct {
	Start    Position
	Operands []Node
}

// Or is a chain a or b or ..., grouped as And is.
type Or struct {
	Start    Position
	Operands []Node
}

// Not is not operand.
type Not struct {
	Start   Position
	Operand Node
}

// Cond is the conditional Then if If else Else.
type Cond struct {
	Start          Position
	If, Then, Else Node
}

// String is a string literal. Value holds the unquoted text.
type String struct {
	Start Position
	Value string
}

// Number is an integer or float literal. Text is the literal as written, a
// leading - included and a leading + left out, so that no digit is lost.
type Number struct {
	Start Position
	Text  string
}

// Bool is True or False.
type Bool struct {
	Start Position
	Value bool
}

// Null is None.
type Null struct {
	Start Position
}

// List is a list literal [a, b, ...].
type List struct {
	Start Position
	Elems []Node
}

// Dict is a dict literal {k: v, ...}.
type Dict struct {
	Start   Position
	Entries []Entry
}

// Entry is an entry k: v of a [Dict].
type Entry struct {
	Key, Value Node
}

func (n Call) Pos() Position   { return n.Start }
func (n And) Pos() Position    { return n.Start }
func (n Or) Pos() Position     { return n.Start }
func (n Not) Pos() Position    { return n.Start }
func (n Cond) Pos() Position   { return n.Start }
func (n String) Pos() Position { return n.Start }
func (n Number) Pos() Position { return n.Start }
func (n Bool) Pos() Position   { return n.Start }
func (n Null) Pos() Position   { return n.Start }
func (n List) Pos() Position   { return n.Start }
func (n Dict) Pos() Position   { return n.Start }

func (Call) node()   {}
func (And) node()    {}
func (Or) node()     {}
func (Not) node()    {}
func (Cond) node()   {}
func (String) node() {}
func (Number) node() {}
func (Bool) node()   {}
func (Null) node()   {}
func (List) node()   {}
func (Dict) node()   {}

// IsName reports whether s is spelled as the name of a call or of a
// keyword arg: an identifier that is no keyword, no reserved word and none
// of None, True and False.
func IsName(s string) bool {
	e, err := (&syntax.FileOptions{}).ParseExpr("", s, 0)
	if err != nil {
		return false
	}

	id, ok := e.(*syntax.Ident)

	return ok && id.Name == s && !isConstant(s)
}

// The text is parsed as the args of a call so that one grammar covers
// keyword args, which are no expressions. The newline ends a comment
// written at the end of the text before it hides the closing parenthesis.
const (
	wrapper = "_"
	prefix  = wrapper + "("
	suffix  = "\n)"
)

// isWrapper reports whether fn is the name of the call Parse wraps the
// text in. The text cannot hold it at line 1, column 1.
func isWrapper(fn syntax.Expr) bool {
	id, ok := fn.(*syntax.Ident)
	return ok && id.Name == wrapper && id.NamePos.Line == 1 && id.NamePos.Col == 1
}

func isConstant(name string) bool {
	return name == "None" || name == "True" || name == "False"
}

type parser struct {
	text string
}

func (p parser) errorf(at syntax.Position, format string, args ...any) error {
	return &Error{Pos: position(p.text, at), Msg: fmt.Sprintf(format, args...)}
}

// unbalanced reports the parenthesis of the text that closes the wrapping
// call early, as in a) or (b.
func (p parser) unbalanced(e syntax.Expr) error {
	at := syntax.Start(e)

	syntax.Walk(e, func(n syntax.Node) bool {
		if c, ok := n.(*syntax.CallExpr); ok && isWrapper(c.Fn) {
			at = c.Rparen
			return false
		}

		return true
	})

	return p.errorf(at, "unbalanced )")
}

// args converts the args of call. The args of a call bind to parameters by
// position, so a positional arg after a keyword arg is an error there. The
// items of the directive bind to nothing by position, and a directive
// written over several lines joins them in any order.
func (p parser) args(call *syntax.CallExpr) ([]Arg, error) {
	var (
		out      []Arg
		keywords = map[string]bool{}
	)

	for _, a := range call.Args {
		arg, err := p.arg(a)
		if err != nil {
			return nil, err
		}

		switch {
		case arg.Name == "" && len(keywords) > 0 && !isWrapper(call.Fn):
			return nil, p.errorf(syntax.Start(a), "positional arg after keyword arg")
		case keywords[arg.Name]:
			return nil, p.errorf(syntax.Start(a), "keyword arg %s repeated", arg.Name)
		case arg.Name != "":
			keywords[arg.Name] = true
		}

		out = append(out, arg)
	}

	return out, nil
}

func (p parser) arg(a syntax.Expr) (Arg, error) {
	if u, ok := a.(*syntax.UnaryExpr); ok && (u.Op == syntax.STAR || u.Op == syntax.STARSTAR) {
		return Arg{}, p.errorf(u.OpPos, "%s arg is not supported", u.Op)
	}

	b, ok := a.(*syntax.BinaryExpr)
	if !ok || b.Op != syntax.EQ {
		v, err := p.node(a)
		return Arg{Start: position(p.text, syntax.Start(a)), Value: v}, err
	}

	// The parser accepts only an identifier before = in an arg.
	id := b.X.(*syntax.Ident)

	if isConstant(id.Name) {
		return Arg{}, p.errorf(id.NamePos, "%s is no name", id.Name)
	}

	v, err := p.node(b.Y)

	return Arg{Start: position(p.text, id.NamePos), Name: id.Name, Value: v}, err
}

func (p parser) node(e syntax.Expr) (Node, error) {
	at := position(p.text, syntax.Start(e))

	switch x := e.(type) {
	case *syntax.Ident:
		return p.ident(x), nil
	case *syntax.CallExpr:
		return p.call(x)
	case *syntax.Literal:
		return p.literal(x)
	case *syntax.UnaryExpr:
		return p.unary(x)
	case *syntax.BinaryExpr:
		switch x.Op {
		case syntax.AND:
			ops, err := p.chain(x, syntax.AND)
			return And{Start: at, Operands: ops}, err
		case syntax.OR:
			ops, err := p.chain(x, syntax.OR)
			return Or{Start: at, Operands: ops}, err
		}

		return nil, p.errorf(x.OpPos, "operator %s is not supported", x.Op)
	case *syntax.ParenExpr:
		if _, ok := x.X.(*syntax.TupleExpr); ok {
			return nil, p.errorf(x.Lparen, "tuple is not supported")
		}

		return p.node(x.X)
	case *syntax.CondExpr:
		return p.cond(x, at)
	case *syntax.ListExpr:
		elems, err := p.nodes(x.List)
		return List{Start: at, Elems: elems}, err
	case *syntax.DictExpr:
		return p.dict(x, at)
	}

	return nil, p.errorf(syntax.Start(e), "%s is not supported", construct(e))
}

func (p parser) ident(x *syntax.Ident) Node {
	at := position(p.text, x.NamePos)

	switch x.Name {
	case "None":
		return Null{Start: at}
	case "True":
		return Bool{Start: at, Value: true}
	case "False":
		return Bool{Start: at}
	}

	return Call{Start: at, Name: x.Name}
}

func (p parser) call(x *syntax.CallExpr) (Node, error) {
	id, ok := x.Fn.(*syntax.Ident)
	if !ok {
		return nil, p.errorf(x.Lparen, "call of %s is not supported", construct(x.Fn))
	}

	if isConstant(id.Name) {
		return nil, p.errorf(id.NamePos, "%s is not callable", id.Name)
	}

	args, err := p.args(x)

	return Call{Start: position(p.text, id.NamePos), Name: id.Name, Parens: true, Args: args}, err
}

func (p parser) literal(x *syntax.Literal) (Node, error) {
	at := position(p.text, x.TokenPos)

	switch x.Token {
	case syntax.STRING:
		return String{Start: at, Value: x.Value.(string)}, nil
	case syntax.INT, syntax.FLOAT:
		return Number{Start: at, Text: x.Raw}, nil
	}

	return nil, p.errorf(x.TokenPos, "%s is not supported", x.Token)
}

func (p parser) unary(x *syntax.UnaryExpr) (Node, error) {
	at := position(p.text, x.OpPos)

	switch x.Op {
	case syntax.NOT:
		v, err := p.node(x.X)
		return Not{Start: at, Operand: v}, err
	case syntax.MINUS, syntax.PLUS:
		lit, ok := x.X.(*syntax.Literal)
		if !ok || lit.Token != syntax.INT && lit.Token != syntax.FLOAT {
			return nil, p.errorf(x.OpPos, "sign %s before no number", x.Op)
		}

		if x.Op == syntax.MINUS {
			return Number{Start: at, Text: "-" + lit.Raw}, nil
		}

		return Number{Start: at, Text: lit.Raw}, nil
	}

	return nil, p.errorf(x.OpPos, "operator %s is not supported", x.Op)
}

// chain flattens a chain of op. A parenthesized operand is a ParenExpr and
// is not flattened.
func (p parser) chain(x *syntax.BinaryExpr, op syntax.Token) ([]Node, error) {
	var out []Node

	for _, side := range []syntax.Expr{x.X, x.Y} {
		if b, ok := side.(*syntax.BinaryExpr); ok && b.Op == op {
			ops, err := p.chain(b, op)
			if err != nil {
				return nil, err
			}

			out = append(out, ops...)

			continue
		}

		n, err := p.node(side)
		if err != nil {
			return nil, err
		}

		out = append(out, n)
	}

	return out, nil
}

func (p parser) cond(x *syntax.CondExpr, at Position) (Node, error) {
	ns, err := p.nodes([]syntax.Expr{x.Cond, x.True, x.False})
	if err != nil {
		return nil, err
	}

	return Cond{Start: at, If: ns[0], Then: ns[1], Else: ns[2]}, nil
}

func (p parser) dict(x *syntax.DictExpr, at Position) (Node, error) {
	out := Dict{Start: at}

	for _, e := range x.List {
		entry := e.(*syntax.DictEntry)

		kv, err := p.nodes([]syntax.Expr{entry.Key, entry.Value})
		if err != nil {
			return nil, err
		}

		out.Entries = append(out.Entries, Entry{Key: kv[0], Value: kv[1]})
	}

	return out, nil
}

func (p parser) nodes(es []syntax.Expr) ([]Node, error) {
	out := make([]Node, 0, len(es))

	for _, e := range es {
		n, err := p.node(e)
		if err != nil {
			return nil, err
		}

		out = append(out, n)
	}

	return out, nil
}

// construct names the Starlark construct of e for an error.
func construct(e syntax.Expr) string {
	switch e.(type) {
	case *syntax.Comprehension:
		return "comprehension"
	case *syntax.LambdaExpr:
		return "lambda"
	case *syntax.DotExpr:
		return "selector"
	case *syntax.IndexExpr:
		return "index"
	case *syntax.SliceExpr:
		return "slice"
	case *syntax.TupleExpr:
		return "tuple"
	case *syntax.CallExpr:
		return "call result"
	}

	return fmt.Sprintf("%T", e)
}

// position converts at, a position in the wrapped text, to a position in
// text. A position in the suffix maps to the end of text.
func position(text string, at syntax.Position) Position {
	src := prefix + text + suffix

	off := 0
	for line := int32(1); line < at.Line && off < len(src); line++ {
		i := strings.IndexByte(src[off:], '\n')
		if i < 0 {
			off = len(src)
			break
		}

		off += i + 1
	}

	for col := int32(1); col < at.Col && off < len(src) && src[off] != '\n'; col++ {
		_, size := utf8.DecodeRuneInString(src[off:])
		off += size
	}

	return positionAt(text, min(max(off-len(prefix), 0), len(text)))
}

// positionAt returns the position of the byte offset off of text.
func positionAt(text string, off int) Position {
	before := text[:off]
	lineStart := strings.LastIndexByte(before, '\n') + 1

	return Position{
		Offset: off,
		Line:   strings.Count(before, "\n") + 1,
		Column: utf8.RuneCountInString(before[lineStart:]) + 1,
	}
}
