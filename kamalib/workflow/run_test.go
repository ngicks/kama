package workflow

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"gotest.tools/v3/assert"
)

type num struct{ V int }

type numParam struct {
	V int `json:"v"`
}

var (
	numType      = reflect.TypeFor[*num]()
	numParamType = reflect.TypeFor[numParam]()
	errBoom      = errors.New("boom")
)

// recorder logs what the executor does, in order.
type recorder struct {
	log []string
}

func (r *recorder) add(s string) { r.log = append(r.log, s) }

// runProc is a proc whose instances come from newInst.
type runProc struct {
	kind     string
	rec      *recorder
	param    reflect.Type
	ins      []reflect.Type
	outs     []reflect.Type
	asInput  reflect.Type
	newInst  func(dryRun bool) Instance
	closeErr error
}

func (p *runProc) ParamType() reflect.Type   { return p.param }
func (p *runProc) InTypes() []reflect.Type   { return p.ins }
func (p *runProc) OutTypes() []reflect.Type  { return p.outs }
func (p *runProc) AsInputType() reflect.Type { return p.asInput }

func (p *runProc) Instantiate(_ context.Context, dryRun bool) (Instance, error) {
	return p.newInst(dryRun), nil
}

func (p *runProc) Close(context.Context) error {
	p.rec.add("close " + p.kind)
	return p.closeErr
}

// numInst hands over a *num holding its param. A negative param passes the
// dry run and fails the real one.
type numInst struct {
	rec    *recorder
	dryRun bool
	p      numParam
	out    num
}

func (i *numInst) Param(p numParam) error {
	if !i.dryRun && p.V < 0 {
		return errors.New("negative")
	}

	i.p = p
	return nil
}

func (i *numInst) In()  {}
func (i *numInst) Out() {}

func (i *numInst) Run(context.Context) error {
	i.rec.add("run num")
	i.out.V = i.p.V
	return nil
}

func (i *numInst) AsInput() *num { return &i.out }

// addInst adds its param to the *num it is given through Out.
type addInst struct {
	rec *recorder
	p   numParam
	dst *num
}

func (i *addInst) Param(p numParam) error { i.p = p; return nil }
func (i *addInst) In()                    {}
func (i *addInst) Out(dst *num)           { i.dst = dst }

func (i *addInst) Run(context.Context) error {
	i.rec.add("run add")
	i.dst.V += i.p.V
	return nil
}

// addUpInst stores the sum of its ins into got.
type addUpInst struct {
	rec  *recorder
	got  *int
	a, b *num
}

func (i *addUpInst) Param() error  { return nil }
func (i *addUpInst) In(a, b *num)  { i.a, i.b = a, b }
func (i *addUpInst) Out()          {}
func (i *addUpInst) AsInput() *num { return &num{V: *i.got} }

func (i *addUpInst) Run(context.Context) error {
	i.rec.add("run sum")
	*i.got = i.a.V + i.b.V
	return nil
}

type failInst struct{ rec *recorder }

func (i *failInst) Param() error { return nil }
func (i *failInst) In()          {}
func (i *failInst) Out()         {}

func (i *failInst) Run(context.Context) error {
	i.rec.add("run fail")
	return errBoom
}

func runProcs(rec *recorder, got *int) map[string]*runProc {
	return map[string]*runProc{
		"num": {
			param:   numParamType,
			asInput: numType,
			newInst: func(dryRun bool) Instance { return &numInst{rec: rec, dryRun: dryRun} },
		},
		"add": {
			param:   numParamType,
			outs:    []reflect.Type{numType},
			newInst: func(bool) Instance { return &addInst{rec: rec} },
		},
		"sum": {
			ins:     []reflect.Type{numType, numType},
			asInput: numType,
			newInst: func(bool) Instance { return &addUpInst{rec: rec, got: got} },
		},
		"fail": {
			newInst: func(bool) Instance { return &failInst{rec: rec} },
		},
	}
}

func buildRun(
	t *testing.T,
	p map[string]*runProc,
	rec *recorder,
	steps map[string]Step,
) *Graph {
	t.Helper()

	reg := NewRegistry()
	for kind, proc := range p {
		proc.kind = kind
		proc.rec = rec
		assert.NilError(t, reg.Register(kind, proc))
	}

	g, err := Spec{Version: SpecVersion, Steps: steps}.Build(context.Background(), reg)
	assert.NilError(t, err)

	return g
}

func TestRunPassesValues(t *testing.T) {
	rec := &recorder{}
	var got int

	g := buildRun(t, runProcs(rec, &got), rec, map[string]Step{
		"a":    {Proc: "num", Param: map[string]any{"v": 2}},
		"b":    {Proc: "num", Param: map[string]any{"v": 3}},
		"bump": {Proc: "add", Out: Names{"a"}, Param: map[string]any{"v": 10}},
		"s":    {Proc: "sum", In: Names{"a", "b"}, After: Names{"bump"}},
	})

	assert.NilError(t, g.Run(context.Background()))
	assert.Equal(t, got, 15)
	assert.DeepEqual(t, rec.log, []string{
		"run num", "run num", "run add", "run sum",
		"close sum", "close add", "close num",
	})
}

func TestRunStopsAtFirstError(t *testing.T) {
	rec := &recorder{}

	g := buildRun(t, runProcs(rec, new(int)), rec, map[string]Step{
		"a": {Proc: "num"},
		"f": {Proc: "fail"},
		"z": {Proc: "num"},
	})

	err := g.Run(context.Background())
	assert.ErrorIs(t, err, ErrRun)
	assert.ErrorIs(t, err, errBoom)
	assert.ErrorContains(t, err, `step "f"`)

	// z is not run, but every proc used so far is closed.
	assert.DeepEqual(t, rec.log, []string{"run num", "run fail", "close fail", "close num"})
}

func TestRunParamFailsOnlyForReal(t *testing.T) {
	rec := &recorder{}

	g := buildRun(t, runProcs(rec, new(int)), rec, map[string]Step{
		"a": {Proc: "num", Param: map[string]any{"v": -1}},
	})

	err := g.Run(context.Background())
	assert.ErrorIs(t, err, ErrParam)
	assert.DeepEqual(t, rec.log, []string{"close num"})
}

func TestRunJoinsCloseErrors(t *testing.T) {
	rec := &recorder{}
	p := runProcs(rec, new(int))
	closeErr := errors.New("close failed")
	p["num"].closeErr = closeErr

	g := buildRun(t, p, rec, map[string]Step{
		"a": {Proc: "num"},
		"f": {Proc: "fail"},
	})

	err := g.Run(context.Background())
	assert.ErrorIs(t, err, errBoom)
	assert.ErrorIs(t, err, closeErr)
}

func TestRunCanceled(t *testing.T) {
	rec := &recorder{}

	g := buildRun(t, runProcs(rec, new(int)), rec, map[string]Step{
		"a": {Proc: "num"},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.ErrorIs(t, g.Run(ctx), context.Canceled)
	assert.Assert(t, len(rec.log) == 0)
}
