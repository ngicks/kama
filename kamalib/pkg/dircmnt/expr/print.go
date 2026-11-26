package expr

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ngicks/opiter"
	"go.starlark.net/syntax"
)

// Print spells args as the args of a directive that [Parse] reads back
// into args, positions aside. It adds parentheses only where the
// precedence of an operator needs them. An And directly inside an And, or
// an Or directly inside an Or, is therefore always parenthesized.
//
// Print does not check the tree. A name for which [IsName] reports false, a
// Number whose Text is no literal, a String that is no valid UTF-8, and an
// And or an Or of fewer than two operands spell text that Parse rejects or
// reads back otherwise.
func Print(args []Arg) string {
	return strings.Join(
		opiter.SlicesValues(args).
			Map(Arg.String).
			CollectSlice(),
		", ",
	)
}

// String spells a as name=value or value.
func (a Arg) String() string {
	if a.Name == "" {
		return a.Value.String()
	}

	return a.Name + "=" + a.Value.String()
}

func (n Call) String() string {
	if !n.Parens && len(n.Args) == 0 {
		return n.Name
	}

	return n.Name + "(" + Print(n.Args) + ")"
}

func (n And) String() string { return chain(n.Operands, " and ", precAnd) }

func (n Or) String() string { return chain(n.Operands, " or ", precOr) }

func (n Not) String() string { return "not " + group(n.Operand, precNot-1) }

func (n Cond) String() string {
	return group(n.Then, precCond) + " if " + group(n.If, precCond) + " else " + n.Else.String()
}

func (n String) String() string { return quote(n.Value) }

func (n Number) String() string { return n.Text }

func (n Bool) String() string {
	if n.Value {
		return "True"
	}

	return "False"
}

func (Null) String() string { return "None" }

func (n List) String() string {
	return "[" + strings.Join(
		opiter.SlicesValues(n.Elems).
			Map(Node.String).
			CollectSlice(),
		", ",
	) + "]"
}

func (n Dict) String() string {
	return "{" + strings.Join(
		opiter.SlicesValues(n.Entries).
			Map(func(e Entry) string { return e.Key.String() + ": " + e.Value.String() }).
			CollectSlice(),
		", ",
	) + "}"
}

// The precedence levels of the tree, from the loosest binding.
const (
	precCond = iota + 1
	precOr
	precAnd
	precNot
	precPrimary
)

func precedence(n Node) int {
	switch n.(type) {
	case Cond:
		return precCond
	case Or:
		return precOr
	case And:
		return precAnd
	case Not:
		return precNot
	}

	return precPrimary
}

// group spells n, parenthesized when it binds no tighter than prec.
func group(n Node, prec int) string {
	if precedence(n) <= prec {
		return "(" + n.String() + ")"
	}

	return n.String()
}

func chain(ops []Node, sep string, prec int) string {
	return strings.Join(
		opiter.SlicesValues(ops).
			Map(func(n Node) string { return group(n, prec) }).
			CollectSlice(),
		sep,
	)
}

// quote spells s as the string literal with the fewest escapes. A raw
// literal keeps a regular expression readable, but it cannot hold its own
// quote, end with a backslash or hold an unprintable character.
func quote(s string) string {
	printable := utf8.ValidString(s) &&
		!strings.ContainsFunc(s, func(r rune) bool { return !unicode.IsPrint(r) })

	if printable {
		for _, q := range []string{`"`, `'`} {
			switch {
			case strings.Contains(s, q):
			case !strings.Contains(s, `\`):
				return q + s + q
			case !strings.HasSuffix(s, `\`):
				return "r" + q + s + q
			}
		}
	}

	return syntax.Quote(s, false)
}
