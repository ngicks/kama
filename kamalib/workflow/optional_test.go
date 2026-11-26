package workflow

import (
	"context"
	"encoding/json/v2"
	"reflect"
	"testing"

	"gotest.tools/v3/assert"
)

// optionalProc is a fakeProc taking optional inputs of type typ.
type optionalProc struct {
	*fakeProc
	typ reflect.Type
}

func (p optionalProc) OptionalType() reflect.Type { return p.typ }

// takeInst takes optional inputs of any type and records them.
type takeInst struct {
	noop
	got *map[string]any
}

func (i takeInst) Optional(in map[string]any) { *i.got = in }

// takeCounterInst takes optional inputs of type *counter.
type takeCounterInst struct{ noop }

func (takeCounterInst) Optional(map[string]*counter) {}

func optionalRegistry(t *testing.T, got *map[string]any) *Registry {
	t.Helper()

	reg := NewRegistry()
	for kind, proc := range newProcs() {
		assert.NilError(t, reg.Register(kind, func() Proc { return proc }))
	}

	register := func(kind string, p Proc) {
		assert.NilError(t, reg.Register(kind, func() Proc { return p }))
	}

	register("take", optionalProc{
		fakeProc: &fakeProc{inst: takeInst{got: got}},
		typ:      reflect.TypeFor[any](),
	})
	register("takeCounter", optionalProc{
		fakeProc: &fakeProc{inst: takeCounterInst{}},
		typ:      counterType,
	})
	// takeNone shares the type of an opting-in proc but takes no optional
	// inputs.
	register("takeNone", optionalProc{fakeProc: &fakeProc{inst: noop{}}})
	// takeMissing opts in but its instance has no Optional method.
	register("takeMissing", optionalProc{
		fakeProc: &fakeProc{inst: noop{}},
		typ:      reflect.TypeFor[any](),
	})

	return reg
}

func buildOptional(t *testing.T, got *map[string]any, steps map[string]Step) (*Graph, error) {
	t.Helper()

	return Spec{Version: SpecVersion, Steps: steps}.
		Build(context.Background(), optionalRegistry(t, got))
}

func TestOptionalOrdersAndDelivers(t *testing.T) {
	var got map[string]any

	g, err := buildOptional(t, &got, map[string]Step{
		"a":    {Proc: "take", Optional: Names{"z", "y"}},
		"y":    {Proc: "double", In: Names{"z"}},
		"z":    {Proc: "seed"},
		"none": {Proc: "take"},
	})
	assert.NilError(t, err)

	// a sorts first by name but waits for the steps it names.
	assert.DeepEqual(t, names(g), []string{"none", "z", "y", "a"})
	assert.DeepEqual(t, node(t, g, "a").Optional, []string{"z", "y"})

	assert.NilError(t, g.Run(context.Background(), ProgressControl{}))
	assert.DeepEqual(t, got, map[string]any{"z": &counter{}, "y": &doubled{}})
}

func TestOptionalEmptyMap(t *testing.T) {
	var got map[string]any

	g, err := buildOptional(t, &got, map[string]Step{"a": {Proc: "take"}})
	assert.NilError(t, err)
	assert.NilError(t, g.Run(context.Background(), ProgressControl{}))
	assert.Assert(t, got != nil)
	assert.Equal(t, len(got), 0)
}

func TestOptionalTypedMap(t *testing.T) {
	_, err := buildOptional(t, nil, map[string]Step{
		"a": {Proc: "takeCounter", Optional: Names{"z"}},
		"z": {Proc: "seed"},
	})
	assert.NilError(t, err)
}

func TestOptionalErrors(t *testing.T) {
	cases := []struct {
		name  string
		steps map[string]Step
		want  error
		msg   string
	}{
		{
			"proc takes none",
			map[string]Step{"a": {Proc: "seed", Optional: Names{"z"}}, "z": {Proc: "seed"}},
			ErrInvalidStep,
			`optional of "a" names 1 steps, but its proc takes no optional inputs`,
		},
		{
			"nil OptionalType",
			map[string]Step{"a": {Proc: "takeNone", Optional: Names{"z"}}, "z": {Proc: "seed"}},
			ErrInvalidStep,
			"takes no optional inputs",
		},
		{
			"unknown step",
			map[string]Step{"a": {Proc: "take", Optional: Names{"nope"}}},
			ErrUnknownStep,
			`"nope" referenced by "a"`,
		},
		{
			"no AsInput",
			map[string]Step{"a": {Proc: "take", Optional: Names{"s"}}, "s": {Proc: "runOnly"}},
			ErrInvalidStep,
			`optional of "a" names "s", which has no AsInput`,
		},
		{
			"type mismatch",
			map[string]Step{
				"a": {Proc: "takeCounter", Optional: Names{"d"}},
				"d": {Proc: "double", In: Names{"z"}},
				"z": {Proc: "seed"},
			},
			ErrTypeMismatch,
			`optional of "a": "d" gives *workflow.doubled, want *workflow.counter`,
		},
		{
			"itself",
			map[string]Step{"a": {Proc: "take", Optional: Names{"a"}}},
			ErrInvalidStep,
			`optional of "a" names the step itself`,
		},
		{
			"twice",
			map[string]Step{"a": {Proc: "take", Optional: Names{"z", "z"}}, "z": {Proc: "seed"}},
			ErrInvalidStep,
			`optional of "a" names "z" twice`,
		},
		{
			"also in",
			map[string]Step{
				"a": {Proc: "double", In: Names{"z"}, Optional: Names{"z"}},
				"z": {Proc: "seed"},
			},
			ErrInvalidStep,
			`optional of "a" names "z", which its in names too`,
		},
		{
			"also out",
			map[string]Step{
				"a": {Proc: "bump", Out: Names{"z"}, Optional: Names{"z"}},
				"z": {Proc: "seed"},
			},
			ErrInvalidStep,
			`optional of "a" names "z", which its out names too`,
		},
		{
			"cycle",
			map[string]Step{
				"a": {Proc: "take", Optional: Names{"b"}},
				"b": {Proc: "double", In: Names{"c"}},
				"c": {Proc: "seed", After: Names{"a"}},
			},
			ErrCycle,
			"among [a b c]",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := buildOptional(t, nil, c.steps)
			assert.ErrorIs(t, err, c.want)
			assert.ErrorContains(t, err, c.msg)
		})
	}
}

func TestOptionalMethodChecked(t *testing.T) {
	_, err := buildOptional(t, nil, map[string]Step{"a": {Proc: "takeMissing"}})
	assert.ErrorIs(t, err, ErrInvalidProc)
	assert.ErrorContains(t, err, "has no Optional method")
}

func TestDecodeOptionalScalar(t *testing.T) {
	var s Step
	assert.NilError(t, json.Unmarshal([]byte(`{"proc":"take","optional":"z"}`), &s))
	assert.DeepEqual(t, s.Optional, Names{"z"})
}
