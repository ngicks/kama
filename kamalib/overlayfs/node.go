package overlayfs

import "context"

// LoadParam configures Load.
type LoadParam struct {
	// Root is the project directory. Empty means the working directory.
	Root string `json:"root"`
}

// Load is the workflow loader that opens the project directory into its
// output slot.
func Load(_ context.Context, out *Fs, p LoadParam) error {
	root := p.Root
	if root == "" {
		root = "."
	}

	return out.Open(root)
}

// CopyDownSink is the workflow sink that copies every staged file into the
// project directory.
func CopyDownSink(ctx context.Context, f *Fs, _ struct{}) error {
	return f.CopyDown(ctx)
}
