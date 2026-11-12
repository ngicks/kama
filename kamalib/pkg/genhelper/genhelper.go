package genhelper

import (
	"fmt"
)

// Context is what a [Step] renders with. C is the implementing type
// itself, so a step hands back the same type it received.
//
// [BaseContext] implements Context. A generator needing more state embeds
// BaseContext and defines WithScope for its own type.
type Context[C Context[C]] interface {
	// Scope returns the lexical scope the next text lands in.
	Scope() *Scope
	// WithScope returns a copy of the context positioned at s.
	WithScope(s *Scope) C
	// Package returns the package the text lands in.
	Package() *Package
	// Printer returns a Printer whose text starts with b.
	Printer(b []byte) Printer
}

// Push returns c positioned at a new scope nested in its current one.
func Push[C Context[C]](c C) C {
	return c.WithScope(c.Scope().Push())
}

// Pop returns c positioned at the scope enclosing its current one.
func Pop[C Context[C]](c C) C {
	return c.WithScope(c.Scope().Parent())
}

// BaseContext is the minimal [Context].
type BaseContext struct {
	scope *Scope
	pkg   *Package
}

var _ Context[BaseContext] = BaseContext{}

func NewBaseContext(s *Scope, d *Package) BaseContext {
	return BaseContext{scope: s, pkg: d}
}

func (c BaseContext) Scope() *Scope { return c.scope }

func (c BaseContext) Package() *Package { return c.pkg }

func (c BaseContext) Printer(b []byte) Printer {
	return newBufPrinter(b)
}

func (c BaseContext) WithScope(s *Scope) BaseContext {
	c.scope = s
	return c
}

// Step is one renderer of a chain. It receives the text the steps before
// it produced and returns the text the steps after it receive. A step
// appends to b, wraps b, or replaces it.
//
// The returned context is the one the next step renders with.
//
// The package defines no error of its own for rendering. A step returns
// what its generator returns, such as the errors of [Package] and
// [Scope.Shadow].
type Step[C Context[C]] interface {
	Render(c C, b []byte) (C, []byte, error)
}

// FuncStep is a function used as a [Step].
type FuncStep[C Context[C]] func(c C, b []byte) (C, []byte, error)

func (f FuncStep[C]) Render(c C, b []byte) (C, []byte, error) {
	return f(c, b)
}

// Steps runs its steps in order. It is a [Step] itself, so chains nest.
//
// Render stops at the first failing step and returns its error wrapped
// with the index of the step. errors.Is and errors.As still match the
// error of the step.
type Steps[C Context[C]] []Step[C]

func (s Steps[C]) Render(c C, b []byte) (C, []byte, error) {
	for i, step := range s {
		var err error
		c, b, err = step.Render(c, b)
		if err != nil {
			return c, nil, fmt.Errorf("genhelper: step %d: %w", i, err)
		}
	}
	return c, b, nil
}

// Renderer is a chain with the context it starts from.
type Renderer[C Context[C]] struct {
	Context C
	Steps   Steps[C]
}

func (r *Renderer[C]) Render(b []byte) (C, []byte, error) {
	return r.Steps.Render(r.Context, b)
}

func (r *Renderer[C]) Prepend(s ...Step[C]) {
	r.Steps = append(s, r.Steps...)
}

func (r *Renderer[C]) Append(s ...Step[C]) {
	r.Steps = append(r.Steps, s...)
}

// Append is a step adding the text of gen after the text before it. It
// returns the error of gen as it is.
func Append[C Context[C]](gen func(c C) (string, error)) Step[C] {
	return FuncStep[C](func(c C, b []byte) (C, []byte, error) {
		s, err := gen(c)
		if err != nil {
			return c, nil, err
		}
		return c, append(b, s...), nil
	})
}

// Wrap is a step replacing the text before it with the text gen builds
// around it.
//
// Wrap runs after the steps whose text it encloses. An identifier those
// steps refer to and Wrap declares is coordinated through [Scope.Use] and
// [Scope.Declare]. [Block] is the alternative that declares first.
//
// Wrap returns the error of gen as it is.
func Wrap[C Context[C]](gen func(c C, inner string) (string, error)) Step[C] {
	return FuncStep[C](func(c C, b []byte) (C, []byte, error) {
		s, err := gen(c, string(b))
		if err != nil {
			return c, nil, err
		}
		return c, []byte(s), nil
	})
}

// Block is a step rendering Body in a nested scope: a function body, a
// loop, a branch or a plain block.
//
// Render pushes a scope, calls Declare, renders Body from empty text, calls
// Wrap with the text of Body, pops the scope and appends the result to the
// text before the block. Declare runs before Body, so Body looks up the
// identifiers the block declares.
//
// Render returns the error of Declare, Body or Wrap as it is.
type Block[C Context[C]] struct {
	// Declare declares the identifiers of the block in the pushed scope,
	// such as loop variables or parameters. It may be nil.
	Declare func(c C) (C, error)
	// Body renders the text inside the block. It may be nil.
	Body Step[C]
	// Wrap builds the block around body. c is positioned at the pushed
	// scope, so c.Scope().Parent() looks up what the block reads from
	// outside.
	Wrap func(c C, body string) (string, error)
}

func (bl Block[C]) Render(c C, b []byte) (C, []byte, error) {
	outer := c.Scope()
	c = Push(c)
	if bl.Declare != nil {
		var err error
		c, err = bl.Declare(c)
		if err != nil {
			return c, nil, err
		}
	}
	var body []byte
	if bl.Body != nil {
		var err error
		c, body, err = bl.Body.Render(c, nil)
		if err != nil {
			return c, nil, err
		}
	}
	s, err := bl.Wrap(c, string(body))
	if err != nil {
		return c, nil, err
	}
	return c.WithScope(outer), append(b, s...), nil
}
