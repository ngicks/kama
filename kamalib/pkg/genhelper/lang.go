package genhelper

// Lang is the vocabulary of steps and texts for the context type C. A
// caller names the context type once, in the type of a Lang value, instead
// of instantiating [Expr], [Stmt], [Name], [Fail] and [FailGen] at every call.
// Most methods are the package-level function of the same name with C fixed.
// [Lang.Case], [Lang.Default] and [Lang.Steps] build values of the types
// [Case] and [Steps].
//
// Lang is zero-size and holds no state, so its zero value is ready to use and
// any number of values behave the same.
type Lang[C Context[C]] struct{}

// Expr is [Expr] for the context type C.
func (Lang[C]) Expr(format string, args ...any) Gen[C] {
	return Expr[C](format, args...)
}

// Stmt is [Stmt] for the context type C.
func (Lang[C]) Stmt(format string, args ...any) Step[C] {
	return Stmt[C](format, args...)
}

// Name is [Name] for the context type C.
func (Lang[C]) Name(role string) Gen[C] {
	return Name[C](role)
}

// Line is [Line] for the context type C.
func (Lang[C]) Line(gen Gen[C]) Step[C] {
	return Line(gen)
}

// Join is [Join] for the context type C.
func (Lang[C]) Join(sep string, parts ...Gen[C]) Gen[C] {
	return Join(sep, parts...)
}

// Comment is [Comment] for the context type C.
func (Lang[C]) Comment(parts ...Gen[C]) Gen[C] {
	return Comment(parts...)
}

// Fail is [Fail] for the context type C.
func (Lang[C]) Fail(err error) Step[C] {
	return Fail[C](err)
}

// FailGen is [FailGen] for the context type C.
func (Lang[C]) FailGen(err error) Gen[C] {
	return FailGen[C](err)
}

// Func is [Func] for the context type C.
func (Lang[C]) Func(signature Gen[C], body ...Step[C]) Step[C] {
	return Func(signature, body...)
}

// DocFunc is [DocFunc] for the context type C.
func (Lang[C]) DocFunc(doc, signature Gen[C], body ...Step[C]) Step[C] {
	return DocFunc(doc, signature, body...)
}

// If is [If] for the context type C.
func (Lang[C]) If(cond Gen[C], then, els Step[C]) Step[C] {
	return If(cond, then, els)
}

// For is [For] for the context type C.
func (Lang[C]) For(init, cond, post Gen[C], body Step[C]) Step[C] {
	return For(init, cond, post, body)
}

// ForRange is [ForRange] for the context type C.
func (Lang[C]) ForRange(key, value string, x Gen[C], body Step[C]) Step[C] {
	return ForRange(key, value, x, body)
}

// Switch is [Switch] for the context type C.
func (Lang[C]) Switch(tag Gen[C], cases ...Case[C]) Step[C] {
	return Switch(tag, cases...)
}

// Case returns the clause of [Switch] whose expressions list renders and
// whose body is body.
func (Lang[C]) Case(list Gen[C], body Step[C]) Case[C] {
	return Case[C]{List: list, Body: body}
}

// Default returns the default clause of [Switch] whose body is body.
func (Lang[C]) Default(body Step[C]) Case[C] {
	return Case[C]{Body: body}
}

// Steps returns steps as a [Steps], which runs them in order.
func (Lang[C]) Steps(steps ...Step[C]) Steps[C] {
	return Steps[C](steps)
}

// Append is [Append] for the context type C.
func (Lang[C]) Append(gen func(c C) (string, error)) Step[C] {
	return Append(gen)
}

// Wrap is [Wrap] for the context type C.
func (Lang[C]) Wrap(gen func(c C, inner string) (string, error)) Step[C] {
	return Wrap(gen)
}

// Render is [Render] for the context type C.
func (Lang[C]) Render(c C, steps ...Step[C]) (string, error) {
	return Render(c, steps...)
}
