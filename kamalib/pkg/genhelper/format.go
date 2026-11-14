package genhelper

import (
	"fmt"
)

// Ref is an argument of [Expr] and [Stmt] spelling the name the role
// refers to in the scope the text renders in, as [Scope.Lookup] finds it.
type Ref string

// Decl is an argument of [Expr] and [Stmt] declaring the role in the scope
// the text renders in, as [Scope.Declare] does, and spelling its name.
type Decl string

// Use is an argument of [Expr] and [Stmt] spelling the name the role
// refers to, as [Scope.Use] does. For a role nothing declares yet, it hands
// out a pending name, which a later [Decl] or [Scope.Declare] of the role in
// the same scope claims. Use serves text that a later step wraps.
type Use string

// Shadow is an argument of [Expr] and [Stmt] declaring the role under
// exactly its name, as [Scope.Shadow] does, and spelling it.
type Shadow string

// Expr returns the text of format formatted with args as fmt.Sprintf
// formats it. Each argument of type [Ref], [Decl], [Use], [Shadow], [Gen]
// or func(C) (string, error) is first replaced with the text it renders in
// the scope of the context. The arguments render in the order they appear,
// so a [Decl] precedes the [Ref] to the same role. Other arguments pass
// through unchanged.
//
// format is source text the generator author writes. Generated text, such
// as a type name or the text of another step, goes in as an argument only,
// so that a % in it is never read as a verb.
//
// The returned Gen returns an error wrapping [ErrUndeclared] for a Ref to a
// role nothing declares, the error of [Scope.Shadow] for a Shadow, and the
// error of a Gen argument as it is.
func Expr[C Context[C]](format string, args ...any) Gen[C] {
	return func(c C) (string, error) {
		out := make([]any, len(args))
		for i, arg := range args {
			var err error
			out[i], err = renderArg(c, arg)
			if err != nil {
				return "", err
			}
		}
		return fmt.Sprintf(format, out...), nil
	}
}

func renderArg[C Context[C]](c C, arg any) (any, error) {
	switch a := arg.(type) {
	case Ref:
		name, ok := c.Scope().Lookup(string(a))
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUndeclared, string(a))
		}
		return name, nil
	case Decl:
		return c.Scope().Declare(string(a)), nil
	case Use:
		return c.Scope().Use(string(a)), nil
	case Shadow:
		return c.Scope().Shadow(string(a))
	case Gen[C]:
		return a(c)
	// A function literal passed as an argument has the unnamed type, which
	// Gen[C] does not match.
	case func(C) (string, error):
		return a(c)
	default:
		return a, nil
	}
}

// Name returns the name role refers to in the scope the text renders in.
// It is Expr("%s", Ref(role)), so it returns an error wrapping
// [ErrUndeclared] for a role nothing declares.
func Name[C Context[C]](role string) Gen[C] {
	return Expr[C]("%s", Ref(role))
}

// Stmt is a step appending the text of [Expr] with format and args,
// followed by a newline. It returns the error of Expr as it is.
func Stmt[C Context[C]](format string, args ...any) Step[C] {
	return Line(Expr[C](format, args...))
}

// Line is a step appending the text of gen followed by a newline. It
// returns the error of gen as it is.
func Line[C Context[C]](gen Gen[C]) Step[C] {
	return FuncStep[C](func(c C, b []byte) (C, []byte, error) {
		s, err := gen(c)
		if err != nil {
			return c, nil, err
		}
		p := c.Printer(b)
		p.Printf("%s\n", s)
		return c, p.Bytes(), nil
	})
}

// Func is a step appending a function:
//
//	signature {
//		body
//	}
//
// signature is everything before the opening brace, such as
// func (p *T) Name(v V) R or the header of a function literal.
//
// Func pushes one scope, where signature and body both render, as the
// parameters and the top-level statements of a Go function body share a
// scope. A [Decl] in signature declares the receiver, a parameter or a
// named result there, and body refers to it with [Ref]. The braces belong
// to Func, and [Block] is the alternative for text that builds its own.
//
// Func returns the error of signature as it is and the error of body as
// [Steps] returns it.
func Func[C Context[C]](signature Gen[C], body ...Step[C]) Step[C] {
	return FuncStep[C](func(c C, b []byte) (C, []byte, error) {
		outer := c.Scope()
		c = Push(c)

		sig, err := signature(c)
		if err != nil {
			return c, nil, err
		}
		p := c.Printer(b)
		p.Printf("%s {\n", sig)
		c, err = printBody(c, p, Steps[C](body))
		if err != nil {
			return c, nil, err
		}
		p.Printf("}\n")
		return c.WithScope(outer), p.Bytes(), nil
	})
}
