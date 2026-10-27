package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"go.yaml.in/yaml/v4"
	"gotest.tools/v3/assert"
)

// counter and doubled are slot types. Formation only looks at their types.
type (
	counter struct{}
	doubled struct{}
)

var (
	counterType = reflect.TypeFor[*counter]()
	doubledType = reflect.TypeFor[*doubled]()
)

type noop struct{}

func (noop) Run(context.Context) error { return nil }

// Instances whose In and Out match the procs declared below.
type (
	seedInst   struct{ noop }
	doubleInst struct{ noop }
	showInst   struct{ noop }
	pairInst   struct{ noop }
	sumInst    struct{ noop }
	// wrongInst declares In with the wrong argument type.
	wrongInst struct{ noop }
)

func (seedInst) Out(*counter)           {}
func (doubleInst) In(*counter)          {}
func (doubleInst) Out(*doubled)         {}
func (showInst) In(*doubled)            {}
func (pairInst) Out(*counter, *doubled) {}
func (sumInst) In(*counter, *doubled)   {}
func (wrongInst) In(*doubled)           {}

type seedParam struct {
	Value int `json:"value"`
}

type fakeProc struct {
	ins, outs []reflect.Type
	inst      Instance
	buildErr  error
	dryRuns   []bool
}

func (p *fakeProc) InTypes() []reflect.Type  { return p.ins }
func (p *fakeProc) OutTypes() []reflect.Type { return p.outs }

func (p *fakeProc) BuildContext(_ context.Context, param Param, dryRun bool) (Instance, error) {
	p.dryRuns = append(p.dryRuns, dryRun)

	if p.buildErr != nil {
		return nil, p.buildErr
	}

	var sp seedParam
	if err := param.Decode(&sp); err != nil {
		return nil, err
	}

	return p.inst, nil
}

type procs struct {
	seed, double, show, pair, sum, wrong, broken *fakeProc
}

func newProcs() procs {
	return procs{
		seed: &fakeProc{outs: []reflect.Type{counterType}, inst: seedInst{}},
		double: &fakeProc{
			ins:  []reflect.Type{counterType},
			outs: []reflect.Type{doubledType},
			inst: doubleInst{},
		},
		show:   &fakeProc{ins: []reflect.Type{doubledType}, inst: showInst{}},
		pair:   &fakeProc{outs: []reflect.Type{counterType, doubledType}, inst: pairInst{}},
		sum:    &fakeProc{ins: []reflect.Type{counterType, doubledType}, inst: sumInst{}},
		wrong:  &fakeProc{ins: []reflect.Type{counterType}, inst: wrongInst{}},
		broken: &fakeProc{outs: []reflect.Type{counterType}, buildErr: errors.New("boom")},
	}
}

func (p procs) registry(t *testing.T) *Registry {
	t.Helper()

	reg := NewRegistry()
	for kind, proc := range map[string]*fakeProc{
		"seed": p.seed, "double": p.double, "show": p.show,
		"pair": p.pair, "sum": p.sum, "wrong": p.wrong, "broken": p.broken,
	} {
		assert.NilError(t, reg.Register(kind, proc))
	}

	return reg
}

func build(t *testing.T, p procs, steps map[string]Step) (*Graph, error) {
	t.Helper()

	return Spec{Version: SpecVersion, Steps: steps}.Build(context.Background(), p.registry(t))
}

func names(g *Graph) []string {
	var out []string
	for _, n := range g.Nodes() {
		out = append(out, n.Name)
	}

	return out
}

// slots renders each slot as owner:type, since go-cmp cannot compare
// reflect.Type values.
func slots(g *Graph) []string {
	var out []string
	for _, s := range g.Slots() {
		out = append(out, s.Owner+":"+s.Type.String())
	}

	return out
}

func node(t *testing.T, g *Graph, name string) Node {
	t.Helper()

	for _, n := range g.Nodes() {
		if n.Name == name {
			return n
		}
	}

	t.Fatalf("no node %q", name)
	return Node{}
}

func TestDefaultSlots(t *testing.T) {
	p := newProcs()

	g, err := build(t, p, map[string]Step{
		"src":  {Proc: "seed", Param: map[string]any{"value": 3}},
		"dbl":  {Proc: "double", In: Names{"src"}},
		"show": {Proc: "show", In: Names{"dbl"}},
	})
	assert.NilError(t, err)

	assert.DeepEqual(t, names(g), []string{"src", "dbl", "show"})
	assert.DeepEqual(t, slots(g), []string{"src:*workflow.counter", "dbl:*workflow.doubled"})

	assert.DeepEqual(t, node(t, g, "src").Out, []int{0})
	assert.DeepEqual(t, node(t, g, "dbl").In, []int{0})
	assert.DeepEqual(t, node(t, g, "dbl").Out, []int{1})
	assert.DeepEqual(t, node(t, g, "show").In, []int{1})

	// Formation builds every proc once, as a dry run.
	assert.DeepEqual(t, p.seed.dryRuns, []bool{true})
	assert.DeepEqual(t, p.double.dryRuns, []bool{true})
	assert.DeepEqual(t, p.show.dryRuns, []bool{true})
}

func TestOutIntoNamedSlot(t *testing.T) {
	g, err := build(t, newProcs(), map[string]Step{
		"src":    {Proc: "seed"},
		"bump":   {Proc: "seed", Out: Names{"src"}},
		"before": {Proc: "double", In: Names{"src"}},
		"after":  {Proc: "double", In: Names{"src"}, After: Names{"bump"}},
		// Naming the writer refers to the slot it writes into.
		"via": {Proc: "double", In: Names{"bump"}},
	})
	assert.NilError(t, err)

	// before has no edge to bump, so name order puts it first.
	assert.DeepEqual(t, names(g), []string{"src", "before", "bump", "after", "via"})

	assert.DeepEqual(t, node(t, g, "bump").Out, []int{0})
	assert.DeepEqual(t, node(t, g, "after").In, []int{0})
	assert.DeepEqual(t, node(t, g, "via").In, []int{0})
	assert.Equal(t, g.Slots()[0].Owner, "src")
}

func TestNamesFlatten(t *testing.T) {
	g, err := build(t, newProcs(), map[string]Step{
		"pair": {Proc: "pair"},
		// pair refers to two slots, which fill both of sum's ins.
		"sum1": {Proc: "sum", In: Names{"pair"}},
		"src":  {Proc: "seed"},
		"dbl":  {Proc: "double", In: Names{"src"}},
		"sum2": {Proc: "sum", In: Names{"src", "dbl"}},
	})
	assert.NilError(t, err)

	assert.DeepEqual(t, node(t, g, "pair").Out, []int{0, 1})
	assert.DeepEqual(t, node(t, g, "sum1").In, []int{0, 1})
	assert.DeepEqual(t, node(t, g, "sum2").In, []int{2, 3})
}

func TestFormationErrors(t *testing.T) {
	cases := []struct {
		name  string
		steps map[string]Step
		want  error
	}{
		{
			"no proc",
			map[string]Step{"x": {}},
			ErrInvalidStep,
		},
		{
			"unknown kind",
			map[string]Step{"x": {Proc: "nope"}},
			ErrUnknownKind,
		},
		{
			"unknown step",
			map[string]Step{"x": {Proc: "double", In: Names{"nope"}}},
			ErrUnknownStep,
		},
		{
			"too many ins",
			map[string]Step{
				"a": {Proc: "seed"},
				"x": {Proc: "seed", In: Names{"a"}},
			},
			ErrArity,
		},
		{
			"too few ins",
			map[string]Step{
				"a": {Proc: "seed"},
				"x": {Proc: "sum", In: Names{"a"}},
			},
			ErrArity,
		},
		{
			"too many outs",
			map[string]Step{
				"a": {Proc: "pair"},
				"x": {Proc: "seed", Out: Names{"a"}},
			},
			ErrArity,
		},
		{
			"in type mismatch",
			map[string]Step{
				"a": {Proc: "seed"},
				"x": {Proc: "show", In: Names{"a"}},
			},
			ErrTypeMismatch,
		},
		{
			"out type mismatch",
			map[string]Step{
				"a": {Proc: "seed"},
				"d": {Proc: "double", In: Names{"a"}},
				"x": {Proc: "seed", Out: Names{"d"}},
			},
			ErrTypeMismatch,
		},
		{
			"step without outs as in",
			map[string]Step{
				"a": {Proc: "seed"},
				"d": {Proc: "double", In: Names{"a"}},
				"s": {Proc: "show", In: Names{"d"}},
				"x": {Proc: "show", In: Names{"s"}},
			},
			ErrInvalidStep,
		},
		{
			"cycle",
			map[string]Step{
				"a": {Proc: "seed", After: Names{"b"}},
				"b": {Proc: "seed", After: Names{"a"}},
			},
			ErrCycle,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := newProcs()

			_, err := build(t, p, c.steps)
			assert.ErrorIs(t, err, c.want)

			// Formation errors stop before any build.
			assert.Equal(t, len(p.seed.dryRuns), 0)
		})
	}

	_, err := Spec{Version: 2}.Build(context.Background(), NewRegistry())
	assert.ErrorIs(t, err, ErrUnsupportedVersion)
}

func TestDryRunErrors(t *testing.T) {
	cases := []struct {
		name  string
		steps map[string]Step
		want  error
	}{
		{
			"build error",
			map[string]Step{"x": {Proc: "broken"}},
			ErrBuild,
		},
		{
			"unknown param field",
			map[string]Step{"x": {Proc: "seed", Param: map[string]any{"valeu": 1}}},
			ErrBuild,
		},
		{
			"param of wrong shape",
			map[string]Step{"x": {Proc: "seed", Param: []any{"./..."}}},
			ErrBuild,
		},
		{
			"In does not match declared types",
			map[string]Step{
				"a": {Proc: "seed"},
				"x": {Proc: "wrong", In: Names{"a"}},
			},
			ErrInvalidProc,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := build(t, newProcs(), c.steps)
			assert.ErrorIs(t, err, c.want)
		})
	}
}

func TestDryRunReportsEveryStep(t *testing.T) {
	_, err := build(t, newProcs(), map[string]Step{
		"a": {Proc: "broken"},
		"b": {Proc: "seed", Param: map[string]any{"valeu": 1}},
	})

	assert.ErrorContains(t, err, `step "a"`)
	assert.ErrorContains(t, err, `step "b"`)
}

func TestMissingMethod(t *testing.T) {
	reg := NewRegistry()
	assert.NilError(
		t,
		reg.Register("seed", &fakeProc{outs: []reflect.Type{counterType}, inst: seedInst{}}),
	)
	// showInst has no Out method, but this proc declares an out.
	assert.NilError(
		t,
		reg.Register("bad", &fakeProc{outs: []reflect.Type{counterType}, inst: showInst{}}),
	)

	_, err := Spec{Version: SpecVersion, Steps: map[string]Step{
		"x": {Proc: "bad"},
	}}.Build(context.Background(), reg)
	assert.ErrorIs(t, err, ErrInvalidProc)
}

func TestRegistryRejects(t *testing.T) {
	reg := NewRegistry()

	assert.ErrorIs(t, reg.Register("a", nil), ErrInvalidProc)
	assert.ErrorIs(
		t,
		reg.Register("a", &fakeProc{outs: []reflect.Type{reflect.TypeFor[counter]()}}),
		ErrInvalidProc,
	)
	assert.ErrorIs(t, reg.Register("a", &fakeProc{ins: []reflect.Type{nil}}), ErrInvalidProc)

	assert.NilError(t, reg.Register("a", &fakeProc{}))
	assert.ErrorIs(t, reg.Register("a", &fakeProc{}), ErrDuplicateKind)
}

func TestDecodeYaml(t *testing.T) {
	src := `
version: 1
steps:
  src:
    proc: seed
    param:
      value: 5
  dbl:
    proc: double
    in: src
  show:
    proc: show
    in: [dbl]
    after: src
`
	var spec Spec
	assert.NilError(t, yaml.Unmarshal([]byte(src), &spec))

	assert.DeepEqual(t, spec.Steps["dbl"].In, Names{"src"})
	assert.DeepEqual(t, spec.Steps["show"].After, Names{"src"})

	g, err := spec.Build(context.Background(), newProcs().registry(t))
	assert.NilError(t, err)
	assert.DeepEqual(t, names(g), []string{"src", "dbl", "show"})
}

func TestDecodeJson(t *testing.T) {
	src := `{"version":1,"steps":{
		"src":{"proc":"seed","param":{"value":2}},
		"dbl":{"proc":"double","in":"src"}
	}}`
	var spec Spec
	assert.NilError(t, json.Unmarshal([]byte(src), &spec))

	g, err := spec.Build(context.Background(), newProcs().registry(t))
	assert.NilError(t, err)
	assert.DeepEqual(t, names(g), []string{"src", "dbl"})
}
