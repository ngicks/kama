// Package builtin registers kama's built-in procs.
//
//	fs          overlayfs.OpenProc       AsInput() *overlayfs.Fs
//	copy_down   overlayfs.CopyDownProc   In(*overlayfs.Fs)
//	gopackages  gopackages.Proc          In(*overlayfs.Fs) AsInput() *gopackages.Packages
//	metadata    metadata.Proc            In(*overlayfs.Fs) AsInput() *metadata.Document
package builtin

import (
	"github.com/ngicks/kama/kamalib/loader/gopackages"
	"github.com/ngicks/kama/kamalib/loader/metadata"
	"github.com/ngicks/kama/kamalib/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
)

// Register adds the built-in procs to reg.
func Register(reg *workflow.Registry) error {
	for _, b := range []struct {
		kind string
		proc workflow.Proc
	}{
		{"fs", overlayfs.OpenProc{}},
		{"copy_down", overlayfs.CopyDownProc{}},
		{"gopackages", gopackages.Proc{}},
		{"metadata", metadata.Proc{}},
	} {
		if err := reg.Register(b.kind, b.proc); err != nil {
			return err
		}
	}

	return nil
}
