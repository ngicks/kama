package base

import (
	"path/filepath"
	"strings"

	"github.com/ngicks/kama/kamalib/pkg/overlayfs/suffix"
)

// sharedPrefix starts the name of every generated file that derives from no
// single source file. It marks the file as generated and sorts it after the
// files written by hand.
const sharedPrefix = "zz_gen."

// Naming names the files of one generator. Name is the generator as file
// names spell it, such as kama_partial.
type Naming struct {
	Name string
}

// Suffix is what Naming inserts into the name of a source file.
func (n Naming) Suffix() string { return "." + n.Name }

// SourceFile names the generated file deriving from the source file src:
// the suffix goes before the build constraint suffixes of its name, as in
// x.kama_partial_linux.go for x_linux.go. The directory of src is kept.
func (n Naming) SourceFile(src string) string {
	return filepath.Join(filepath.Dir(src), suffix.SuffixFilename(filepath.Base(src), n.Suffix()))
}

// SharedFile names the file of a package holding the declarations of kind
// its generated files share, as in zz_gen.helper.kama_partial.go.
func (n Naming) SharedFile(kind string) string {
	return sharedPrefix + kind + n.Suffix() + ".go"
}

// SharedFileOf names the file holding the declarations of kind that only
// generated files under the build constraint of the source file src share.
// It keeps the name of src so that two constrained source files of a
// package get a file each, and so that a //go:build line, which no file
// name shows, still tells the files apart:
// zz_gen.helper.x.kama_partial_linux.go for x_linux.go. The directory of
// src is kept.
func (n Naming) SharedFileOf(kind, src string) string {
	return filepath.Join(
		filepath.Dir(src),
		sharedPrefix+kind+"."+suffix.SuffixFilename(filepath.Base(src), n.Suffix()),
	)
}

// IsShared reports whether base, a file name without a directory, is one
// SharedFile or SharedFileOf gives for kind.
func (n Naming) IsShared(kind, base string) bool {
	return strings.HasPrefix(base, sharedPrefix+kind+".") && suffix.IsSuffixed(base, n.Suffix())
}

// Constrained reports whether the name of the source file src carries a
// build constraint suffix.
func Constrained(src string) bool {
	base := filepath.Base(src)
	return suffix.SuffixFilename(base, ".x") != strings.TrimSuffix(base, ".go")+".x.go"
}
