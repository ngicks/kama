package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/loader/metadata"
)

// counter and doubled are slot types; a loader writes into one and later
// steps read it.
type (
	counter struct{ n int }
	doubled struct{ n int }
)

type seedParam struct {
	Value int `json:"value"`
}

func seed(_ context.Context, out *counter, p seedParam) error {
	out.n = p.Value
	return nil
}

func double(_ context.Context, in *counter, out *doubled, _ struct{}) error {
	out.n = in.n * 2
	return nil
}

// bump writes into an existing counter slot.
func bump(_ context.Context, out *counter, p seedParam) error {
	out.n += p.Value
	return nil
}

func failing(context.Context, *counter, struct{}) error {
	return errors.New("boom")
}

type recorder struct{ seen []int }

func (r *recorder) counter(_ context.Context, in *counter, _ struct{}) error {
	r.seen = append(r.seen, in.n)
	return nil
}

func (r *recorder) doubled(_ context.Context, in *doubled, _ struct{}) error {
	r.seen = append(r.seen, in.n)
	return nil
}

func testRegistry(t *testing.T, rec *recorder) *Registry {
	t.Helper()

	reg := NewRegistry()
	assert.NilError(t, reg.Loader("seed", seed))
	assert.NilError(t, reg.Loader("double", double))
	assert.NilError(t, reg.Loader("bump", bump))
	assert.NilError(t, reg.Loader("failing", failing))
	assert.NilError(t, reg.Sink("counter", rec.counter))
	assert.NilError(t, reg.Sink("doubled", rec.doubled))

	return reg
}

func build(t *testing.T, rec *recorder, steps map[string]Step) (*Plan, error) {
	t.Helper()

	return Spec{Version: SpecVersion, Steps: steps}.Build(testRegistry(t, rec))
}

func TestDefaultSlotChain(t *testing.T) {
	rec := &recorder{}

	p, err := build(t, rec, map[string]Step{
		"src":  {Loader: "seed", Param: map[string]any{"value": 3}},
		"dbl":  {Loader: "double", Input: Names{"src"}},
		"show": {Sink: "doubled", Input: Names{"dbl"}},
	})
	assert.NilError(t, err)

	assert.DeepEqual(t, p.Names(), []string{"src", "dbl", "show"})

	assert.NilError(t, p.Run(context.Background()))
	assert.DeepEqual(t, rec.seen, []int{6})
}

func TestOutputWritesIntoNamedSlot(t *testing.T) {
	rec := &recorder{}

	p, err := build(t, rec, map[string]Step{
		"src":    {Loader: "seed", Param: map[string]any{"value": 3}},
		"bump":   {Loader: "bump", Output: "src", Param: map[string]any{"value": 4}},
		"before": {Sink: "counter", Input: Names{"src"}},
		"after":  {Sink: "counter", Input: Names{"src"}, After: Names{"bump"}},
		// Reading the writer gives its slot, which is the slot it wrote into.
		"via": {Sink: "counter", Input: Names{"bump"}},
	})
	assert.NilError(t, err)

	// before has no edge to bump, so name order puts it first.
	assert.DeepEqual(t, p.Names(), []string{"src", "before", "bump", "after", "via"})

	assert.NilError(t, p.Run(context.Background()))
	assert.DeepEqual(t, rec.seen, []int{3, 7, 7})
}

func TestRunIsRepeatable(t *testing.T) {
	rec := &recorder{}

	p, err := build(t, rec, map[string]Step{
		"src":  {Loader: "seed", Param: map[string]any{"value": 1}},
		"bump": {Loader: "bump", Output: "src", Param: map[string]any{"value": 1}},
		"show": {Sink: "counter", Input: Names{"src"}, After: Names{"bump"}},
	})
	assert.NilError(t, err)

	assert.NilError(t, p.Run(context.Background()))
	assert.NilError(t, p.Run(context.Background()))

	// Each run allocates fresh slots.
	assert.DeepEqual(t, rec.seen, []int{2, 2})
}

func TestBuildErrors(t *testing.T) {
	cases := []struct {
		name  string
		steps map[string]Step
		want  error
	}{
		{
			"neither loader nor sink",
			map[string]Step{"x": {}},
			ErrInvalidStep,
		},
		{
			"both loader and sink",
			map[string]Step{"x": {Loader: "seed", Sink: "counter"}},
			ErrInvalidStep,
		},
		{
			"unknown loader",
			map[string]Step{"x": {Loader: "nope"}},
			ErrUnknownKind,
		},
		{
			"unknown step",
			map[string]Step{"x": {Loader: "double", Input: Names{"nope"}}},
			ErrUnknownStep,
		},
		{
			"too many inputs",
			map[string]Step{
				"a": {Loader: "seed"},
				"x": {Loader: "seed", Input: Names{"a"}},
			},
			ErrArity,
		},
		{
			"input type mismatch",
			map[string]Step{
				"a": {Loader: "seed"},
				"x": {Sink: "doubled", Input: Names{"a"}},
			},
			ErrTypeMismatch,
		},
		{
			"output type mismatch",
			map[string]Step{
				"a": {Loader: "seed"},
				"d": {Loader: "double", Input: Names{"a"}},
				"x": {Loader: "bump", Output: "d"},
			},
			ErrTypeMismatch,
		},
		{
			"sink as input",
			map[string]Step{
				"a": {Loader: "seed"},
				"s": {Sink: "counter", Input: Names{"a"}},
				"x": {Sink: "counter", Input: Names{"s"}},
			},
			ErrInvalidStep,
		},
		{
			"sink with output",
			map[string]Step{
				"a": {Loader: "seed"},
				"x": {Sink: "counter", Input: Names{"a"}, Output: "a"},
			},
			ErrInvalidStep,
		},
		{
			"cycle",
			map[string]Step{
				"a": {Loader: "seed", After: Names{"b"}},
				"b": {Loader: "seed", After: Names{"a"}},
			},
			ErrCycle,
		},
		{
			"unknown param field",
			map[string]Step{"x": {Loader: "seed", Param: map[string]any{"valeu": 1}}},
			ErrParam,
		},
		{
			"param of wrong shape",
			map[string]Step{"x": {Loader: "seed", Param: []any{"./..."}}},
			ErrParam,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := build(t, &recorder{}, c.steps)
			assert.ErrorIs(t, err, c.want)
		})
	}

	_, err := Spec{Version: 2}.Build(NewRegistry())
	assert.ErrorIs(t, err, ErrUnsupportedVersion)
}

func TestRunError(t *testing.T) {
	p, err := build(t, &recorder{}, map[string]Step{
		"a": {Loader: "seed"},
		"x": {Loader: "failing", Output: "a"},
	})
	assert.NilError(t, err)

	assert.ErrorContains(t, p.Run(context.Background()), `step "x": boom`)
}

func TestRegistryRejects(t *testing.T) {
	reg := NewRegistry()

	assert.ErrorIs(t, reg.Loader("a", 1), ErrInvalidFunc)
	assert.ErrorIs(
		t,
		reg.Loader("a", func(*counter, struct{}) error { return nil }),
		ErrInvalidFunc,
	)
	assert.ErrorIs(
		t,
		reg.Loader("a", func(context.Context, counter, struct{}) error { return nil }),
		ErrInvalidFunc,
	)
	assert.ErrorIs(t, reg.Loader("a", func(context.Context, *counter, struct{}) {}), ErrInvalidFunc)
	assert.ErrorIs(
		t,
		reg.Sink("a", func(context.Context, ...int) error { return nil }),
		ErrInvalidFunc,
	)

	assert.NilError(t, reg.Loader("a", seed))
	assert.ErrorIs(t, reg.Loader("a", seed), ErrDuplicateKind)
}

func TestDecodeYaml(t *testing.T) {
	src := `
version: 1
steps:
  src:
    loader: seed
    param:
      value: 5
  dbl:
    loader: double
    input: src
  show:
    sink: doubled
    input: [dbl]
`
	var spec Spec
	assert.NilError(t, metadata.Decode(strings.NewReader(src), metadata.FormatYaml, &spec))

	assert.DeepEqual(t, spec.Steps["dbl"].Input, Names{"src"})

	rec := &recorder{}
	p, err := spec.Build(testRegistry(t, rec))
	assert.NilError(t, err)

	assert.NilError(t, p.Run(context.Background()))
	assert.DeepEqual(t, rec.seen, []int{10})
}

func TestDecodeJson(t *testing.T) {
	src := `{"version":1,"steps":{
		"src":{"loader":"seed","param":{"value":2}},
		"show":{"sink":"counter","input":"src"}
	}}`
	var spec Spec
	assert.NilError(t, metadata.Decode(strings.NewReader(src), metadata.FormatJson, &spec))

	rec := &recorder{}
	p, err := spec.Build(testRegistry(t, rec))
	assert.NilError(t, err)

	assert.NilError(t, p.Run(context.Background()))
	assert.DeepEqual(t, rec.seen, []int{2})
}
