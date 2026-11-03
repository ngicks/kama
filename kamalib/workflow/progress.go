package workflow

import "sync"

// Messages Graph.Run reports through ProgressControl.OnProgress for each
// step it executes.
const (
	progressStart  = "start"
	progressDone   = "done"
	progressFailed = "failed"
)

// ProgressControl receives what a run reports. Nil fields are ignored.
//
// Graph.Run never calls the fields concurrently, so they need not be safe
// for concurrent use.
type ProgressControl struct {
	OnWarning  func(step string, w error)
	OnProgress func(step string, msg string)
}

// Reporter is handed to each instance's Run. It tags what the instance
// reports with the step name and passes it to the run's ProgressControl.
//
// A Reporter is safe for concurrent use, so an instance may report from
// goroutines it starts. The executor closes the Reporter after the
// instance's Run returns. Closing waits for calls in flight to finish and
// makes later calls no-ops. No report from the instance reaches
// ProgressControl after the step's "done" or "failed" progress.
type Reporter interface {
	// Warn reports a problem that does not fail the step. A nil w is
	// ignored.
	Warn(w error)
	// Progress reports a short message on how the step is going.
	Progress(msg string)
}

// progress serializes the calls a run makes to its ProgressControl.
type progress struct {
	mu sync.Mutex
	pc ProgressControl
}

func (p *progress) warn(step string, w error) {
	if p.pc.OnWarning == nil || w == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.pc.OnWarning(step, w)
}

func (p *progress) progress(step, msg string) {
	if p.pc.OnProgress == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.pc.OnProgress(step, msg)
}

// stepReporter is the Reporter for one step.
type stepReporter struct {
	p    *progress
	step string

	mu   sync.Mutex
	done bool
}

func (r *stepReporter) Warn(w error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.done {
		r.p.warn(r.step, w)
	}
}

func (r *stepReporter) Progress(msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.done {
		r.p.progress(r.step, msg)
	}
}

// close makes r drop every later call. It waits for calls in flight, so no
// callback runs for the step once close returns.
func (r *stepReporter) close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.done = true
}
