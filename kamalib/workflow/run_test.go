package workflow

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
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
	kind       string
	rec        *recorder
	param      reflect.Type
	ins        []reflect.Type
	outs       []reflect.Type
	asInput    reflect.Type
	newInst    func(dryRun bool) Instance
	cleanUpErr error
}

func (p *runProc) ParamType() reflect.Type   { return p.param }
func (p *runProc) InTypes() []reflect.Type   { return p.ins }
func (p *runProc) OutTypes() []reflect.Type  { return p.outs }
func (p *runProc) AsInputType() reflect.Type { return p.asInput }

func (p *runProc) Instantiate(_ context.Context, dryRun bool) (Instance, error) {
	return p.newInst(dryRun), nil
}

func (p *runProc) CleanUp(context.Context) error {
	p.rec.add("clean up " + p.kind)
	return p.cleanUpErr
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

func (i *numInst) Run(context.Context, Reporter) error {
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

func (i *addInst) Run(context.Context, Reporter) error {
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

func (i *addUpInst) Run(context.Context, Reporter) error {
	i.rec.add("run sum")
	*i.got = i.a.V + i.b.V
	return nil
}

type failInst struct{ rec *recorder }

func (i *failInst) Param() error { return nil }
func (i *failInst) In()          {}
func (i *failInst) Out()         {}

func (i *failInst) Run(context.Context, Reporter) error {
	i.rec.add("run fail")
	return errBoom
}

var errWarn = errors.New("careful")

// reportInst warns and reports progress, from its own goroutine as well.
type reportInst struct{ rec *recorder }

func (i *reportInst) Param() error { return nil }
func (i *reportInst) In()          {}
func (i *reportInst) Out()         {}

func (i *reportInst) Run(_ context.Context, r Reporter) error {
	i.rec.add("run report")
	r.Progress("half")
	r.Warn(nil)

	var eg errgroup.Group
	eg.Go(func() error {
		r.Warn(errWarn)
		return nil
	})
	_ = eg.Wait()

	r.Progress("all")
	return nil
}

// burstInst reports from several goroutines at once.
type burstInst struct{}

func (i *burstInst) Param() error { return nil }
func (i *burstInst) In()          {}
func (i *burstInst) Out()         {}

const (
	burstGoroutines = 8
	burstReports    = 100
)

func (i *burstInst) Run(_ context.Context, r Reporter) error {
	var eg errgroup.Group
	for range burstGoroutines {
		eg.Go(func() error {
			for range burstReports {
				r.Warn(errWarn)
				r.Progress("tick")
			}
			return nil
		})
	}
	return eg.Wait()
}

// blockInst reports "blocked" from a goroutine that outlives its Run, and
// returns once the callback for that report has been entered. The goroutine
// reports "late" after the callback is released.
type blockInst struct {
	entered  <-chan struct{}
	returned chan<- struct{}
	bg       *errgroup.Group
	err      error
}

func (i *blockInst) Param() error { return nil }
func (i *blockInst) In()          {}
func (i *blockInst) Out()         {}

func (i *blockInst) Run(_ context.Context, r Reporter) error {
	defer close(i.returned)

	i.bg.Go(func() error {
		r.Progress("blocked")
		r.Progress("late")
		return nil
	})
	<-i.entered

	return i.err
}

// leakInst keeps its Reporter and reports after its Run returned.
type leakInst struct{ leaked *Reporter }

func (i *leakInst) Param() error { return nil }
func (i *leakInst) In()          {}
func (i *leakInst) Out()         {}

func (i *leakInst) Run(_ context.Context, r Reporter) error {
	*i.leaked = r
	return nil
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
		"report": {
			newInst: func(bool) Instance { return &reportInst{rec: rec} },
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
		assert.NilError(t, reg.Register(kind, func() Proc { return proc }))
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

	assert.NilError(t, g.Run(context.Background(), ProgressControl{}))
	assert.Equal(t, got, 15)
	assert.DeepEqual(t, rec.log, []string{
		"run num", "run num", "run add", "run sum",
		"clean up sum", "clean up add", "clean up num",
	})
}

func TestRunStopsAtFirstError(t *testing.T) {
	rec := &recorder{}

	g := buildRun(t, runProcs(rec, new(int)), rec, map[string]Step{
		"a": {Proc: "num"},
		"f": {Proc: "fail"},
		"z": {Proc: "num"},
	})

	err := g.Run(context.Background(), ProgressControl{})
	assert.ErrorIs(t, err, ErrRun)
	assert.ErrorIs(t, err, errBoom)
	assert.ErrorContains(t, err, `step "f"`)

	// z is not run, but every proc used so far is cleaned up.
	assert.DeepEqual(t, rec.log, []string{"run num", "run fail", "clean up fail", "clean up num"})
}

func TestRunParamFailsOnlyForReal(t *testing.T) {
	rec := &recorder{}

	g := buildRun(t, runProcs(rec, new(int)), rec, map[string]Step{
		"a": {Proc: "num", Param: map[string]any{"v": -1}},
	})

	err := g.Run(context.Background(), ProgressControl{})
	assert.ErrorIs(t, err, ErrParam)
	assert.DeepEqual(t, rec.log, []string{"clean up num"})
}

func TestRunJoinsCleanUpErrors(t *testing.T) {
	rec := &recorder{}
	p := runProcs(rec, new(int))
	cleanUpErr := errors.New("clean up failed")
	p["num"].cleanUpErr = cleanUpErr

	g := buildRun(t, p, rec, map[string]Step{
		"a": {Proc: "num"},
		"f": {Proc: "fail"},
	})

	err := g.Run(context.Background(), ProgressControl{})
	assert.ErrorIs(t, err, errBoom)
	assert.ErrorIs(t, err, cleanUpErr)
}

func TestRunCanceled(t *testing.T) {
	rec := &recorder{}

	g := buildRun(t, runProcs(rec, new(int)), rec, map[string]Step{
		"a": {Proc: "num"},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.ErrorIs(t, g.Run(ctx, ProgressControl{}), context.Canceled)
	assert.Assert(t, len(rec.log) == 0)
}

// progressLog is a ProgressControl recording every call as one line.
func progressLog(log *[]string) ProgressControl {
	return ProgressControl{
		OnWarning: func(step string, w error) {
			*log = append(*log, step+": warning: "+w.Error())
		},
		OnProgress: func(step, msg string) {
			*log = append(*log, step+": "+msg)
		},
	}
}

func TestRunReportsProgress(t *testing.T) {
	rec := &recorder{}
	var log []string

	g := buildRun(t, runProcs(rec, new(int)), rec, map[string]Step{
		"a": {Proc: "num"},
		"r": {Proc: "report", After: Names{"a"}},
	})

	assert.NilError(t, g.Run(context.Background(), progressLog(&log)))
	assert.DeepEqual(t, log, []string{
		"a: start",
		"a: done",
		"r: start",
		"r: half",
		"r: warning: careful",
		"r: all",
		"r: done",
	})
}

func TestRunReportsFailedStep(t *testing.T) {
	rec := &recorder{}
	var log []string

	g := buildRun(t, runProcs(rec, new(int)), rec, map[string]Step{
		"a": {Proc: "num", Param: map[string]any{"v": -1}},
		"f": {Proc: "fail"},
	})

	err := g.Run(context.Background(), progressLog(&log))
	assert.ErrorIs(t, err, ErrParam)
	// a fails before its Run, and f is not started.
	assert.DeepEqual(t, log, []string{"a: start", "a: failed"})

	log = nil
	g = buildRun(t, runProcs(rec, new(int)), rec, map[string]Step{
		"f": {Proc: "fail"},
	})

	err = g.Run(context.Background(), progressLog(&log))
	assert.ErrorIs(t, err, errBoom)
	assert.DeepEqual(t, log, []string{"f: start", "f: failed"})
}

func TestRunIgnoresNilCallbacks(t *testing.T) {
	rec := &recorder{}

	g := buildRun(t, runProcs(rec, new(int)), rec, map[string]Step{
		"r": {Proc: "report"},
		"f": {Proc: "fail", After: Names{"r"}},
	})

	assert.ErrorIs(t, g.Run(context.Background(), ProgressControl{}), errBoom)
	assert.DeepEqual(t, rec.log, []string{
		"run report", "run fail", "clean up fail", "clean up report",
	})
}

func TestRunDropsReportsAfterRun(t *testing.T) {
	rec := &recorder{}
	var (
		log    []string
		leaked Reporter
	)

	g := buildRun(t, map[string]*runProc{
		"leak": {newInst: func(bool) Instance { return &leakInst{leaked: &leaked} }},
	}, rec, map[string]Step{
		"l": {Proc: "leak"},
	})

	assert.NilError(t, g.Run(context.Background(), progressLog(&log)))

	leaked.Warn(errWarn)
	leaked.Progress("late")
	assert.DeepEqual(t, log, []string{"l: start", "l: done"})
}

func TestRunSerializesCallbacks(t *testing.T) {
	var (
		active     atomic.Int32
		overlapped atomic.Bool
		calls      atomic.Int32
	)
	enter := func() {
		calls.Add(1)
		if active.Add(1) > 1 {
			overlapped.Store(true)
		}
		// Yielding widens the window in which an unserialized call would
		// enter alongside this one.
		runtime.Gosched()
		active.Add(-1)
	}

	g := buildRun(t, map[string]*runProc{
		"burst": {newInst: func(bool) Instance { return &burstInst{} }},
	}, &recorder{}, map[string]Step{
		"b": {Proc: "burst"},
	})

	assert.NilError(t, g.Run(context.Background(), ProgressControl{
		OnWarning:  func(string, error) { enter() },
		OnProgress: func(string, string) { enter() },
	}))
	assert.Assert(t, !overlapped.Load())
	// start and done, plus a warning and a progress per report round.
	assert.Equal(t, calls.Load(), int32(2+burstGoroutines*burstReports*2))
}

func TestRunEndsStepAfterCallbackInFlight(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		terminal string
	}{
		{name: "done", terminal: "b: done"},
		{name: "failed", err: errBoom, terminal: "b: failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var (
				log      []string
				bg       errgroup.Group
				entered  = make(chan struct{})
				returned = make(chan struct{})
				release  = make(chan struct{})
			)

			g := buildRun(t, map[string]*runProc{
				"block": {newInst: func(bool) Instance {
					return &blockInst{entered: entered, returned: returned, bg: &bg, err: tc.err}
				}},
			}, &recorder{}, map[string]Step{
				"b": {Proc: "block"},
			})

			pc := ProgressControl{
				OnProgress: func(step, msg string) {
					log = append(log, step+": "+msg)
					if msg == "blocked" {
						close(entered)
						<-release
						log = append(log, step+": released")
					}
				},
			}

			var eg errgroup.Group
			eg.Go(func() error { return g.Run(context.Background(), pc) })

			<-returned
			// The sleep gives the executor time to reach the reporter's close
			// while the callback is still blocked. The assertions hold however
			// long it takes.
			time.Sleep(10 * time.Millisecond)
			close(release)

			err := eg.Wait()
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
			} else {
				assert.NilError(t, err)
			}
			assert.NilError(t, bg.Wait())

			// "late" races with the reporter's close, so it may or may not be
			// delivered. When it is, it comes before the terminal progress.
			trimmed := slices.DeleteFunc(slices.Clone(log), func(s string) bool {
				return s == "b: late"
			})
			assert.DeepEqual(t, trimmed, []string{
				"b: start", "b: blocked", "b: released", tc.terminal,
			})
			assert.Equal(t, log[len(log)-1], tc.terminal)
		})
	}
}
