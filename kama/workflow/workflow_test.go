package workflow

import (
	"context"
	"errors"
	"slices"
	"testing"
)

type fakeNode struct {
	name     string
	requires []KeyRef
	provides []KeyRef
	run      func(in *Inputs, out *Outputs, emit Emitter) error
	runs     int
}

func (n *fakeNode) Name() string       { return n.name }
func (n *fakeNode) Requires() []KeyRef { return n.requires }
func (n *fakeNode) Provides() []KeyRef { return n.provides }
func (n *fakeNode) Run(_ context.Context, in *Inputs, out *Outputs, emit Emitter) error {
	n.runs++
	if n.run == nil {
		return nil
	}
	return n.run(in, out, emit)
}

type fakeEmitter struct{ names []string }

func (e *fakeEmitter) Emit(_ context.Context, name string, _ []byte) error {
	e.names = append(e.names, name)
	return nil
}

var (
	keySrc    = NewKey[int]("src", nil)
	keyDouble = NewKey[int]("double", nil)
)

// source provides keySrc with a value that changes on every run, so a rerun is
// observable downstream.
func source() *fakeNode {
	n := &fakeNode{name: "source", provides: []KeyRef{keySrc}}
	n.run = func(_ *Inputs, out *Outputs, _ Emitter) error {
		return Set(out, keySrc, n.runs)
	}
	return n
}

func doubler() *fakeNode {
	return &fakeNode{
		name:     "double",
		requires: []KeyRef{keySrc},
		provides: []KeyRef{keyDouble},
		run: func(in *Inputs, out *Outputs, _ Emitter) error {
			v, err := Get(in, keySrc)
			if err != nil {
				return err
			}
			return Set(out, keyDouble, v*2)
		},
	}
}

func emitter(name string) *fakeNode {
	return &fakeNode{
		name:     name,
		requires: []KeyRef{keySrc},
		run: func(_ *Inputs, _ *Outputs, emit Emitter) error {
			return emit.Emit(context.Background(), name+".go", nil)
		},
	}
}

func consumer(name string, k Key[int], seen *[]int) *fakeNode {
	return &fakeNode{
		name:     name,
		requires: []KeyRef{k},
		run: func(in *Inputs, _ *Outputs, _ Emitter) error {
			v, err := Get(in, k)
			if err != nil {
				return err
			}
			*seen = append(*seen, v)
			return nil
		},
	}
}

func mustRun(t *testing.T, nodes ...Node) *fakeEmitter {
	t.Helper()
	p, err := New(nodes...)
	if err != nil {
		t.Fatal(err)
	}
	e := &fakeEmitter{}
	if err := p.Run(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestBranchAndJoin(t *testing.T) {
	var seen []int
	src := source()
	join := &fakeNode{
		name:     "join",
		requires: []KeyRef{keySrc, keyDouble},
		run: func(in *Inputs, _ *Outputs, _ Emitter) error {
			a, _ := Get(in, keySrc)
			b, _ := Get(in, keyDouble)
			seen = append(seen, a, b)
			return nil
		},
	}
	mustRun(t, src, doubler(), consumer("c", keySrc, &seen), join)
	if src.runs != 1 {
		t.Errorf("source ran %d times, want 1", src.runs)
	}
	if want := []int{1, 1, 2}; !slices.Equal(seen, want) {
		t.Errorf("seen %v, want %v", seen, want)
	}
}

func TestReloadAfterEmit(t *testing.T) {
	var seen []int
	src := source()
	dbl := doubler()
	e := mustRun(t,
		src,
		dbl,
		consumer("before", keyDouble, &seen),
		emitter("gen"),
		consumer("after", keyDouble, &seen),
		consumer("again", keyDouble, &seen),
	)
	if want := []string{"gen.go"}; !slices.Equal(e.names, want) {
		t.Errorf("emitted %v, want %v", e.names, want)
	}
	if src.runs != 2 || dbl.runs != 2 {
		t.Errorf("source ran %d, doubler ran %d, want 2 and 2", src.runs, dbl.runs)
	}
	if want := []int{2, 4, 4}; !slices.Equal(seen, want) {
		t.Errorf("seen %v, want %v", seen, want)
	}
}

func TestNearestEarlierProvider(t *testing.T) {
	var seen []int
	override := &fakeNode{
		name:     "override",
		provides: []KeyRef{keySrc},
		run: func(_ *Inputs, out *Outputs, _ Emitter) error {
			return Set(out, keySrc, 100)
		},
	}
	mustRun(t, source(), consumer("a", keySrc, &seen), override, consumer("b", keySrc, &seen))
	if want := []int{1, 100}; !slices.Equal(seen, want) {
		t.Errorf("seen %v, want %v", seen, want)
	}
}

func TestDefaultInsertedOnce(t *testing.T) {
	var (
		defaults []*fakeNode
		key      Key[int]
	)
	key = NewKey[int]("withdef", func() Node {
		n := &fakeNode{name: "default", provides: []KeyRef{key}}
		n.run = func(_ *Inputs, out *Outputs, _ Emitter) error { return Set(out, key, 7) }
		defaults = append(defaults, n)
		return n
	})
	var seen []int
	p, err := New(consumer("a", key, &seen), consumer("b", key, &seen))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, 3)
	for _, n := range p.Nodes() {
		names = append(names, n.Name())
	}
	if want := []string{"default", "a", "b"}; !slices.Equal(names, want) {
		t.Fatalf("planned %v, want %v", names, want)
	}
	if err := p.Run(context.Background(), &fakeEmitter{}); err != nil {
		t.Fatal(err)
	}
	if len(defaults) != 1 || defaults[0].runs != 1 {
		t.Errorf("default constructed %d times, want 1", len(defaults))
	}
	if want := []int{7, 7}; !slices.Equal(seen, want) {
		t.Errorf("seen %v, want %v", seen, want)
	}
}

func TestErrors(t *testing.T) {
	var seen []int
	_, err := New(consumer("a", keySrc, &seen))
	if !errors.Is(err, ErrNoProvider) {
		t.Errorf("no provider: got %v", err)
	}

	bad := &fakeNode{name: "bad", provides: []KeyRef{keySrc}}
	p, err := New(bad)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Run(context.Background(), &fakeEmitter{}); !errors.Is(err, ErrMissingOutput) {
		t.Errorf("missing output: got %v", err)
	}

	undeclared := &fakeNode{name: "undeclared"}
	undeclared.run = func(_ *Inputs, out *Outputs, _ Emitter) error { return Set(out, keySrc, 1) }
	p, err = New(undeclared)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Run(context.Background(), &fakeEmitter{}); !errors.Is(err, ErrNotDeclared) {
		t.Errorf("not declared: got %v", err)
	}

	wrongType := &fakeNode{name: "wrong", requires: []KeyRef{keySrc}}
	wrongType.run = func(in *Inputs, _ *Outputs, _ Emitter) error {
		_, err := Get(in, NewKey[string]("src", nil))
		return err
	}
	p, err = New(source(), wrongType)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Run(context.Background(), &fakeEmitter{}); !errors.Is(err, ErrTypeMismatch) {
		t.Errorf("type mismatch: got %v", err)
	}
}
