package overlayfs

import (
	"context"

	"github.com/ngicks/kama/kama/workflow"
)

// Provide returns the node that opens root and provides *Fs.
func Provide(root string) workflow.Node { return &provider{root: root} }

type provider struct {
	root string
}

func (*provider) Requires() []workflow.KeyRef { return nil }
func (*provider) Provides() []workflow.KeyRef { return []workflow.KeyRef{workflow.Key[*Fs]{}} }

func (p *provider) Run(_ context.Context, _ *workflow.Inputs, out *workflow.Outputs) error {
	f, err := New(p.root)
	if err != nil {
		return err
	}

	return out.Set(f)
}

// Sink returns the output node: it writes every file routed to it into the
// top layer and provides *Fs again so later nodes can depend on the written
// state.
func Sink() workflow.Node { return sink{} }

type sink struct{}

func (sink) Requires() []workflow.KeyRef { return []workflow.KeyRef{workflow.Key[*Fs]{}} }
func (sink) Provides() []workflow.KeyRef { return []workflow.KeyRef{workflow.Key[*Fs]{}} }

func (sink) Run(_ context.Context, in *workflow.Inputs, out *workflow.Outputs) error {
	f, err := in.Get[*Fs]()
	if err != nil {
		return err
	}

	for _, file := range in.Emitted() {
		if err := f.Write(file.Path, file.Content); err != nil {
			return err
		}
	}

	return out.Set(f)
}
