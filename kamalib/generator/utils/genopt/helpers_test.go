package genopt

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ngicks/opiter"

	"golang.org/x/tools/go/packages"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
)

// testRoot is the root the packages of the tests lie under. No file is read
// from it.
var testRoot = filepath.FromSlash("/proj")

// parsedPackage returns the package pkgPath of the files, keyed by name,
// in dir, a slash-separated path relative to testRoot.
func parsedPackage(t *testing.T, pkgPath, dir string, files map[string]string) *packages.Package {
	t.Helper()

	p := &packages.Package{
		ID:      pkgPath,
		PkgPath: pkgPath,
		Dir:     filepath.Join(testRoot, filepath.FromSlash(dir)),
		Fset:    token.NewFileSet(),
	}

	for _, name := range slices.Sorted(maps.Keys(files)) {
		f, err := parser.ParseFile(
			p.Fset, filepath.Join(p.Dir, name), files[name], parser.ParseComments,
		)
		assert.NilError(t, err)

		p.Syntax = append(p.Syntax, f)
	}

	return p
}

// typedPackage returns parsedPackage type-checked. The files import
// nothing.
func typedPackage(t *testing.T, pkgPath, dir string, files map[string]string) *packages.Package {
	t.Helper()

	p := parsedPackage(t, pkgPath, dir, files)

	var err error

	p.Types, err = (&types.Config{}).Check(pkgPath, p.Fset, p.Syntax, nil)
	assert.NilError(t, err)

	return p
}

// leaf is the leaf of the tests. It records what the loader hands it.
type leaf struct {
	Value string   `json:"value,omitzero"`
	List  []int    `json:"list,omitzero"`
	Sub   *subLeaf `json:"sub,omitzero"`
	// Unknown captures the keys of an option file entry the leaf lacks.
	Unknown map[string]any `json:",embed"`
	// directives are the args of each directive, spelled key or key=value
	// and joined by spaces, keyed by directive name.
	directives map[string]string
	// at is the position of each directive in call order, as
	// <file base name>:<line>:<column> <target>.
	at []string
	// from is the source SetSource gives.
	from string
}

// subLeaf is a table nested in a leaf.
type subLeaf struct {
	Value   string         `json:"value,omitzero"`
	Unknown map[string]any `json:",embed"`
}

var errLeaf = errors.New("leaf refused")

func (l *leaf) UnmarshalKeyValue(src Source, directive string, kv []dircmnt.KeyValue) error {
	if directive == "fail" {
		return fmt.Errorf("%w: %s", errLeaf, src.Pos)
	}

	if l.directives == nil {
		l.directives = map[string]string{}
	}

	l.directives[directive] = strings.Join(
		opiter.WrapSeq(slices.Values(kv)).
			Map(spellKeyValue).
			CollectSlice(),
		" ",
	)
	l.at = append(l.at, fmt.Sprintf(
		"%s:%d:%d %s",
		filepath.Base(src.Pos.Filename), src.Pos.Line, src.Pos.Column, src.Target.Describe(),
	))

	return nil
}

func (l *leaf) SetSource(src Source) { l.from = relSource(src) }

func (l *leaf) UnknownKeys() [][]string {
	keys := UnknownKeys(l.Unknown)
	if l.Sub != nil {
		keys = append(keys, UnknownKeys(l.Sub.Unknown, "sub")...)
	}

	return keys
}

func spellKeyValue(kv dircmnt.KeyValue) string {
	if kv.HasValue {
		return kv.Key + "=" + kv.Value
	}

	return kv.Key
}

// String spells the parts of l that are set.
func (l leaf) String() string {
	var parts []string
	if l.Value != "" {
		parts = append(parts, "value="+l.Value)
	}

	if l.List != nil {
		parts = append(parts, fmt.Sprintf("list=%v", l.List))
	}

	if l.Sub != nil {
		parts = append(parts, "sub="+l.Sub.Value)
	}

	for _, name := range slices.Sorted(maps.Keys(l.directives)) {
		parts = append(parts, fmt.Sprintf("[%s]%s", name, l.directives[name]))
	}

	if len(l.at) > 0 {
		parts = append(parts, "at "+strings.Join(l.at, ", "))
	}

	if l.from != "" {
		parts = append(parts, "from "+l.from)
	}

	return strings.Join(parts, "; ")
}

// relSource spells src with the file names relative to testRoot.
func relSource(src Source) string {
	return strings.ReplaceAll(src.String(), testRoot+string(filepath.Separator), "")
}

type testLoaded = Loaded[leaf, leaf, leaf]

// flatten spells o as one line per leaf and per type, keyed by
//
//	<pkg> package
//	<pkg> file <file>
//	<pkg>.<type>             source and whether fields are some
//	<pkg>.<type> group
//	<pkg>.<type> type
//	<pkg>.<type>.<field>
//
// Leaves spelling nothing are left out.
func flatten(o Option[leaf, leaf, leaf]) map[string]string {
	out := map[string]string{}
	put := func(key string, l leaf) {
		if s := l.String(); s != "" {
			out[key] = s
		}
	}

	for pkgPath, po := range o.Packages {
		put(pkgPath+" package", po.Package)

		for file, p := range po.Files {
			put(pkgPath+" file "+filepath.Base(file), p)
		}

		for name, to := range po.Types {
			key := pkgPath + "." + name

			fields, ok := to.Fields.Unpack()
			out[key] = fmt.Sprintf("%s fields=%t", relSource(to.Source), ok)

			put(key+" group", to.Group)
			put(key+" type", to.Type)

			for field, f := range fields {
				put(key+"."+field, f)
			}
		}
	}

	return out
}

// nodeKinds returns the kinds of the nodes of the type targets of o, keyed
// by <pkg>.<type>.
func nodeKinds(o Option[leaf, leaf, leaf]) map[string]string {
	out := map[string]string{}

	for pkgPath, po := range o.Packages {
		for name, to := range po.Types {
			var kind string

			switch to.Source.Target.Node.(type) {
			case *ast.TypeSpec:
				kind = "TypeSpec"
			case nil:
				kind = "nil"
			default:
				kind = fmt.Sprintf("%T", to.Source.Target.Node)
			}

			out[pkgPath+"."+name] = kind
		}
	}

	return out
}
