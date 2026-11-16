package genhelper

import (
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/opiter"
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

// NewBaseContext returns a BaseContext positioned at s, rendering for d.
// When d has Imports, NewBaseContext links the scope tree of s with them
// through [Scope.LinkImports], so that locals and import identifiers do not
// collide.
func NewBaseContext(s *Scope, d *Package) BaseContext {
	if s != nil && d != nil && d.Imports != nil {
		s.LinkImports(d.Imports)
	}
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
// A step returns what its generator returns, such as the errors of
// [Package], [Scope.Shadow] and [Expr].
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
// with the index of the step, as in "genhelper: step 3: <error>". A failing
// Steps nested directly in another, with no wrapping between them, extends
// the path instead: "genhelper: step 3.0: <error>" is step 0 of the Steps
// that is step 3. errors.Is and errors.As still match the error of the
// step.
type Steps[C Context[C]] []Step[C]

func (s Steps[C]) Render(c C, b []byte) (C, []byte, error) {
	for i, step := range s {
		var err error
		c, b, err = step.Render(c, b)
		if err != nil {
			return c, nil, newStepError(i, err)
		}
	}
	return c, b, nil
}

// Fail is a step failing with err as it is, for a branch chosen while the
// steps are built that has no text to give. [Steps] wraps err with the path
// of the step as it wraps any other error.
func Fail[C Context[C]](err error) Step[C] {
	return FuncStep[C](func(c C, _ []byte) (C, []byte, error) {
		return c, nil, err
	})
}

// Render renders steps in order from c and empty text and returns the
// text. It discards the context the steps return.
//
// Render returns the error of steps as [Steps] returns it, so the error
// names the index of the failing step.
func Render[C Context[C]](c C, steps ...Step[C]) (string, error) {
	_, b, err := Steps[C](steps).Render(c, nil)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// stepError is the error of a step of [Steps], with the index of the step
// in each enclosing Steps.
type stepError struct {
	path []int
	err  error
}

func newStepError(i int, err error) error {
	// A type assertion instead of errors.As keeps a message a step wraps
	// around a nested stepError, which extending the path would drop.
	if se, ok := err.(*stepError); ok {
		return &stepError{path: slices.Concat([]int{i}, se.path), err: se.err}
	}
	return &stepError{path: []int{i}, err: err}
}

func (e *stepError) Error() string {
	var sb strings.Builder
	sb.WriteString("genhelper: step ")
	indices := opiter.WrapSeq(
		slices.Values(e.path),
	).
		Map(strconv.Itoa).
		Collect()
	sb.WriteString(strings.Join(indices, "."))
	sb.WriteString(": ")
	sb.WriteString(e.err.Error())
	return sb.String()
}

func (e *stepError) Unwrap() error {
	return e.err
}

// Renderer is a chain with the context it starts from.
type Renderer[C Context[C]] struct {
	Context C
	Steps   Steps[C]
}

// Render renders Steps from Context and b. It returns the errors
// [Steps.Render] returns.
func (r *Renderer[C]) Render(b []byte) (C, []byte, error) {
	return r.Steps.Render(r.Context, b)
}

// Prepend adds s before Steps. It leaves s itself unmodified.
func (r *Renderer[C]) Prepend(s ...Step[C]) {
	r.Steps = slices.Concat(s, r.Steps)
}

// Append adds s after Steps.
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
	// outside. A nil Wrap gives the text of Body as it is.
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
	if bl.Wrap == nil {
		return c.WithScope(outer), append(b, body...), nil
	}
	s, err := bl.Wrap(c, string(body))
	if err != nil {
		return c, nil, err
	}
	return c.WithScope(outer), append(b, s...), nil
}
