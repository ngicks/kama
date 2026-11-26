// Package expr parses the args of a directive comment as Starlark
// expressions into a small tree, and prints the tree back.
//
// The text after //kama:validate in
//
//	//kama:validate required, length(min=1, max=3), not regexp(r"^x-") or kind("null")
//
// is the args of one directive. [Parse] reads them with the parser of
// go.starlark.net/syntax and evaluates nothing. It returns the items of the
// args as [Arg] values holding nodes of this package. [Print] spells a tree
// back. [Object] binds the args of a [Call] to the parameters of a rule and
// returns the literal ones as a JSON object.
//
// # Grammar
//
// Parse reads the text as the arguments of a Starlark call. The grammar,
// in the notation of the Go spec, is:
//
//	args    = [ arg { "," arg } [ "," ] ] .
//	arg     = [ name "=" ] expr .
//	expr    = or [ "if" or "else" expr ] .
//	or      = and { "or" and } .
//	and     = not { "and" not } .
//	not     = "not" not | primary .
//	primary = call | literal | list | dict | "(" expr ")" .
//	call    = name [ "(" args ")" ] .
//	literal = string | [ "-" | "+" ] number | "True" | "False" | "None" .
//	list    = "[" [ expr { "," expr } [ "," ] ] "]" .
//	dict    = "{" [ expr ":" expr { "," expr ":" expr } [ "," ] ] "}" .
//
// Inside the parentheses of a call, every positional arg comes before the
// first keyword arg, because positional args bind to parameters by
// position. The items of the directive itself bind to nothing by position,
// so they mix positional and keyword items in any order. A keyword appears
// at most once among the items of the directive and at most once among the
// args of each call.
//
// The conditional binds loosest, then or, then and, and not binds tightest.
// A conditional requires its else part. Parentheses group an operand that
// binds looser than its operator. A conditional used as the Then or the If
// of another conditional needs them too.
//
// A name is a Starlark identifier. The following words cannot be names.
// The Starlark keywords are and, break, continue, def, elif, else, for, if,
// in, lambda, load, not, or, pass, return and while. The words reserved by
// Starlark are as, async, await, class, del, except, finally, from, global,
// import, is, nonlocal, raise, try, with and yield. None, True and False
// are literals. [IsName] reports whether a word can be a name.
//
// A string is a Starlark string literal. It is quoted with " or ', or
// tripled, and it takes the escapes of Starlark. The prefix r makes a raw
// string, which keeps every backslash, as in r"^\d+$". A raw string
// cannot end with a backslash. A number is a Starlark integer in decimal, hexadecimal 0x, octal 0o
// or
// binary 0b, or a float such as 0.5, .5, 1. or 1e-3. The Starlark scanner
// rejects a float that the float64 type cannot hold, such as 1e400.
//
// A # outside a string starts a comment that ends at the end of the line.
// A newline may stand wherever a space may, so text joined from several
// comment lines with newlines parses as one directive. A newline does not
// separate items, so each line but the last ends with a comma when the
// next line starts a new item.
//
// # Tree
//
// A bare name is a [Call] without args. The Parens field tells f() from f.
// A chain of and becomes one [And], and a chain of or becomes one [Or]. A
// parenthesized chain stays one operand, so a and (b and c) gives an And
// that holds an And. Parentheses leave no other trace in the tree. A
// leading - becomes part of the Text of the [Number] it precedes. Parse
// drops a leading +.
//
// Every other construct of Starlark is an error. The errors cover every
// comparison and every other binary operator, every unary operator other
// than not, a sign before anything but a number literal, a tuple, a
// selector, an index, a slice, a comprehension, a lambda, a bytes literal,
// the call of anything but a name, and *args and **kwargs. Each error of
// Parse, those of the Starlark parser included, is an [*Error] that wraps
// [ErrSyntax] and holds the [Position] in the text. The Starlark parser
// may locate an error just after the token at fault.
//
// The tree has no notion of rules. A caller resolves each Call by its
// name, and decides what a bare name such as off in hook=off means.
package expr
