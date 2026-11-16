package workflow

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/ngicks/opiter"
	"go.yaml.in/yaml/v4"
	"gotest.tools/v3/assert"
)

// counter and doubled are values steps hand over. Formation only looks at
// their types.
type (
	counter struct{}
	doubled struct{}
)

var (
	counterType = reflect.TypeFor[*counter]()
	doubledType = reflect.TypeFor[*doubled]()
)

type seedParam struct {
	Value int `json:"value"`
}

var seedParamType = reflect.TypeFor[seedParam]()

type noop struct{}

func (noop) Param() error                        { return nil }
func (noop) In()                                 {}
func (noop) Out()                                {}
func (noop) Run(context.Context, Reporter) error { return nil }

// runOnly has none of the mandatory Param, In and Out methods.
type runOnly struct{}

func (runOnly) Run(context.Context, Reporter) error { return nil }

// Instances whose methods match the procs declared in newProcs.
type (
	seedInst   struct{ noop }
	doubleInst struct{ noop }
	showInst   struct{ noop }
	bumpInst   struct{ noop }
	sumInst    struct{ noop }
	// wrongInst declares In with the wrong argument type.
	wrongInst struct{ noop }
	// paramNoErrInst declares Param without the error result.
	paramNoErrInst struct{ noop }
)

func (seedInst) Param(p seedParam) error {
	if p.Value < 0 {
		return errors.New("value must not be negative")
	}

	return nil
}

func (seedInst) AsInput() *counter     { return &counter{} }
func (doubleInst) In(*counter)         {}
func (doubleInst) AsInput() *doubled   { return &doubled{} }
func (showInst) In(*doubled)           {}
func (bumpInst) Out(*counter)          {}
func (sumInst) In(*counter, *doubled)  {}
func (wrongInst) In(*doubled)          {}
func (paramNoErrInst) Param(seedParam) {}

type fakeProc struct {
	param   reflect.Type
	ins     []reflect.Type
	outs    []reflect.Type
	asInput reflect.Type
	inst    Instance
	instErr error
	dryRuns []bool
}

func (p *fakeProc) ParamType() reflect.Type       { return p.param }
func (p *fakeProc) InTypes() []reflect.Type       { return p.ins }
func (p *fakeProc) OutTypes() []reflect.Type      { return p.outs }
func (p *fakeProc) AsInputType() reflect.Type     { return p.asInput }
func (p *fakeProc) CleanUp(context.Context) error { return nil }

func (p *fakeProc) Instantiate(_ context.Context, dryRun bool) (Instance, error) {
	p.dryRuns = append(p.dryRuns, dryRun)

	if p.instErr != nil {
		return nil, p.instErr
	}

	return p.inst, nil
}

type procs map[string]*fakeProc

func newProcs() procs {
	return procs{
		"seed": {param: seedParamType, asInput: counterType, inst: seedInst{}},
		"double": {
			ins:     []reflect.Type{counterType},
			asInput: doubledType,
			inst:    doubleInst{},
		},
		"show":   {ins: []reflect.Type{doubledType}, inst: showInst{}},
		"bump":   {outs: []reflect.Type{counterType}, inst: bumpInst{}},
		"sum":    {ins: []reflect.Type{counterType, doubledType}, inst: sumInst{}},
		"wrong":  {ins: []reflect.Type{counterType}, inst: wrongInst{}},
		"broken": {asInput: counterType, instErr: errors.New("boom")},
		// noAsInput declares an AsInput its instance lacks.
		"noAsInput":  {asInput: counterType, inst: showInst{}},
		"runOnly":    {inst: runOnly{}},
		"paramNoErr": {param: seedParamType, inst: paramNoErrInst{}},
	}
}

func (p procs) registry(t *testing.T) *Registry {
	t.Helper()

	reg := NewRegistry()
	for kind, proc := range p {
		assert.NilError(t, reg.Register(kind, func() Proc { return proc }))
	}

	return reg
}

func build(t *testing.T, p procs, steps map[string]Step) (*Graph, error) {
	t.Helper()

	return Spec{Version: SpecVersion, Steps: steps}.Build(context.Background(), p.registry(t))
}

func names(g *Graph) []string {
	return opiter.WrapSeq(
		slices.Values(g.Nodes()),
	).
		Map(
			func(n Node) string { return n.Name },
		).
		Collect()
}

func node(t *testing.T, g *Graph, name string) Node {
	t.Helper()

	found := opiter.WrapSeq(
		slices.Values(g.Nodes()),
	).
		Filter(
			func(n Node) bool { return n.Name == name },
		).
		First()
	if found.IsNone() {
		t.Fatalf("no node %q", name)
	}

	return found.Value()
}

func TestFormation(t *testing.T) {
	p := newProcs()

	g, err := build(t, p, map[string]Step{
		"src":  {Proc: "seed", Param: map[string]any{"value": 3}},
		"dbl":  {Proc: "double", In: Names{"src"}},
		"show": {Proc: "show", In: Names{"dbl"}},
		"sum":  {Proc: "sum", In: Names{"src", "dbl"}},
	})
	assert.NilError(t, err)

	assert.DeepEqual(t, names(g), []string{"src", "dbl", "show", "sum"})
	assert.DeepEqual(t, node(t, g, "sum").In, []string{"src", "dbl"})

	param, ok := reflect.TypeAssert[seedParam](node(t, g, "src").Param)
	assert.Assert(t, ok)
	assert.Equal(t, param.Value, 3)
	assert.Assert(t, !node(t, g, "dbl").Param.IsValid())

	// Formation instantiates every step once, as a dry run.
	assert.DeepEqual(t, p["seed"].dryRuns, []bool{true})
	assert.DeepEqual(t, p["double"].dryRuns, []bool{true})
	assert.DeepEqual(t, p["show"].dryRuns, []bool{true})
}

func TestOutWritesIntoNamedStep(t *testing.T) {
	g, err := build(t, newProcs(), map[string]Step{
		"src":    {Proc: "seed"},
		"bump":   {Proc: "bump", Out: Names{"src"}},
		"before": {Proc: "double", In: Names{"src"}},
		"after":  {Proc: "double", In: Names{"src"}, After: Names{"bump"}},
	})
	assert.NilError(t, err)

	// before has no edge to bump, so name order puts it first.
	assert.DeepEqual(t, names(g), []string{"src", "before", "bump", "after"})
	assert.DeepEqual(t, node(t, g, "bump").Out, []string{"src"})
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
				"a": {Proc: "seed"},
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
				"x": {Proc: "bump", Out: Names{"d"}},
			},
			ErrTypeMismatch,
		},
		{
			"step without AsInput named",
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

			// Formation errors stop before any instantiation.
			assert.Equal(t, len(p["seed"].dryRuns), 0)
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
			"instantiate error",
			map[string]Step{"x": {Proc: "broken"}},
			ErrInstantiate,
		},
		{
			"param of wrong shape",
			map[string]Step{"x": {Proc: "seed", Param: []any{"./..."}}},
			ErrParam,
		},
		{
			"param for a proc without ParamType",
			map[string]Step{
				"a": {Proc: "seed"},
				"x": {Proc: "bump", Out: Names{"a"}, Param: map[string]any{"a": 1}},
			},
			ErrParam,
		},
		{
			"param rejected by Param",
			map[string]Step{"x": {Proc: "seed", Param: map[string]any{"value": -1}}},
			ErrParam,
		},
		{
			"In does not match declared types",
			map[string]Step{
				"a": {Proc: "seed"},
				"x": {Proc: "wrong", In: Names{"a"}},
			},
			ErrInvalidProc,
		},
		{
			"AsInput missing",
			map[string]Step{"x": {Proc: "noAsInput"}},
			ErrInvalidProc,
		},
		{
			"Param, In and Out missing",
			map[string]Step{"x": {Proc: "runOnly"}},
			ErrInvalidProc,
		},
		{
			"Param without error result",
			map[string]Step{"x": {Proc: "paramNoErr"}},
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

func TestParamIgnoresUnknownFields(t *testing.T) {
	g, err := build(t, newProcs(), map[string]Step{
		"x": {Proc: "seed", Param: map[string]any{"value": 1, "extra": true}},
	})
	assert.NilError(t, err)

	got, ok := reflect.TypeAssert[seedParam](g.Nodes()[0].Param)
	assert.Assert(t, ok)
	assert.Equal(t, got, seedParam{Value: 1})
}

func TestParamMatchesKeysCaseSensitively(t *testing.T) {
	g, err := build(t, newProcs(), map[string]Step{
		"x": {Proc: "seed", Param: map[string]any{"Value": 1}},
	})
	assert.NilError(t, err)

	got, ok := reflect.TypeAssert[seedParam](g.Nodes()[0].Param)
	assert.Assert(t, ok)
	assert.Equal(t, got, seedParam{})
}

func TestParamRejectsInvalidUtf8(t *testing.T) {
	_, err := build(t, newProcs(), map[string]Step{
		"x": {Proc: "seed", Param: map[string]any{"extra": "\xff"}},
	})
	assert.ErrorIs(t, err, ErrParam)
	assert.ErrorContains(t, err, "invalid UTF-8")
}

func TestDryRunReportsEveryStep(t *testing.T) {
	_, err := build(t, newProcs(), map[string]Step{
		"a": {Proc: "broken"},
		"b": {Proc: "seed", Param: map[string]any{"value": -1}},
	})

	assert.ErrorContains(t, err, `step "a"`)
	assert.ErrorContains(t, err, `step "b"`)
}

func TestRegistryRejects(t *testing.T) {
	reg := NewRegistry()

	assert.ErrorIs(t, reg.Register("a", nil), ErrInvalidProc)

	newProc := func() Proc { return &fakeProc{} }
	assert.NilError(t, reg.Register("a", newProc))
	assert.ErrorIs(t, reg.Register("a", newProc), ErrDuplicateKind)
}

func TestBuildRejectsInvalidProc(t *testing.T) {
	cases := []struct {
		name    string
		newProc func() Proc
	}{
		{"nil proc", func() Proc { return nil }},
		{"nil in type", func() Proc { return &fakeProc{ins: []reflect.Type{nil}} }},
		{"nil out type", func() Proc { return &fakeProc{outs: []reflect.Type{nil}} }},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reg := NewRegistry()
			assert.NilError(t, reg.Register("a", c.newProc))

			_, err := Spec{
				Version: SpecVersion,
				Steps:   map[string]Step{"x": {Proc: "a"}},
			}.Build(context.Background(), reg)
			assert.ErrorIs(t, err, ErrInvalidProc)
		})
	}
}

func TestBuildConstructsProcsPerGraph(t *testing.T) {
	var built int
	reg := NewRegistry()
	assert.NilError(t, reg.Register("show", func() Proc {
		built++
		return &fakeProc{inst: noop{}}
	}))

	spec := Spec{
		Version: SpecVersion,
		Steps: map[string]Step{
			"a": {Proc: "show"},
			"b": {Proc: "show"},
		},
	}

	g1, err := spec.Build(context.Background(), reg)
	assert.NilError(t, err)
	g2, err := spec.Build(context.Background(), reg)
	assert.NilError(t, err)

	// Steps of one graph share a proc; graphs do not.
	assert.Equal(t, built, 2)
	assert.Equal(t, g1.Nodes()[0].Proc, g1.Nodes()[1].Proc)
	assert.Assert(t, g1.Nodes()[0].Proc != g2.Nodes()[0].Proc)
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

func TestDecodeJsonNames(t *testing.T) {
	cases := []struct {
		src  string
		want Names
	}{
		{`{"in":"a"}`, Names{"a"}},
		{`{"in":["a","b"]}`, Names{"a", "b"}},
		{`{"in":[]}`, Names{}},
		{`{"in":null}`, nil},
		{`{}`, nil},
	}

	for _, c := range cases {
		var st Step
		assert.NilError(t, json.Unmarshal([]byte(c.src), &st), c.src)
		assert.DeepEqual(t, st.In, c.want)
	}
}

func TestDecodeJsonNamesErrors(t *testing.T) {
	for _, src := range []string{`{"in":1}`, `{"in":["a",1]}`, `{"in":{}}`} {
		var st Step
		err := json.Unmarshal([]byte(src), &st)

		var semErr *json.SemanticError
		assert.Assert(t, errors.As(err, &semErr), "%s: %v", src, err)
	}
}

func TestDecodeJsonRejectsDuplicateNames(t *testing.T) {
	var spec Spec
	err := json.Unmarshal([]byte(`{"version":1,"version":1}`), &spec)

	var synErr *jsontext.SyntacticError
	assert.Assert(t, errors.As(err, &synErr), "%v", err)
}
