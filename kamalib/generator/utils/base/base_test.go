package base

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"path/filepath"
	"testing"
	"time"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/genmark"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
)

func TestId(t *testing.T) {
	// The namespace differs from ToolName so that a mix-up of the two shows.
	id := Id{Namespace: "acme", Name: "foo"}
	assert.Equal(t, id.String(), "acme:foo")
	assert.Equal(t, id.Marker(), "//kama:generated acme:foo")
	assert.Equal(t, id.Directive(), "//acme:foo")
	assert.Equal(t, id.Naming(), Naming{Name: "acme_foo"})

	reg := genmark.NewRegistry(ToolName)
	assert.NilError(t, reg.Register(id.String()))

	f, err := parser.ParseFile(
		token.NewFileSet(),
		"x.go",
		"package x\n\n"+id.Marker()+"\ntype X int\n",
		parser.ParseComments,
	)
	assert.NilError(t, err)

	gen, ok := reg.Generated(f.Decls[0].(*ast.GenDecl).Doc)
	assert.Assert(t, ok)
	assert.Equal(t, gen, id.String())
}

func TestNaming(t *testing.T) {
	n := Naming{Name: "kama_x"}
	assert.Equal(t, n.Suffix(), ".kama_x")
	assert.Equal(t, n.SharedFile("helper"), "zz_gen.helper.kama_x.go")

	for _, c := range []struct{ src, file, shared string }{
		{"d/a.go", "d/a.kama_x.go", "d/zz_gen.helper.a.kama_x.go"},
		{"d/a_linux.go", "d/a.kama_x_linux.go", "d/zz_gen.helper.a.kama_x_linux.go"},
		{
			"d/a_linux_amd64.go",
			"d/a.kama_x_linux_amd64.go",
			"d/zz_gen.helper.a.kama_x_linux_amd64.go",
		},
		{"d/a_tagged.go", "d/a_tagged.kama_x.go", "d/zz_gen.helper.a_tagged.kama_x.go"},
	} {
		assert.Equal(t, n.SourceFile(c.src), filepath.FromSlash(c.file))
		assert.Equal(t, n.SharedFileOf("helper", c.src), filepath.FromSlash(c.shared))
		assert.Assert(t, n.IsShared("helper", path.Base(c.shared)), c.shared)
		assert.Assert(t, !n.IsShared("helper", path.Base(c.file)), c.file)
		assert.Assert(t, !n.IsShared("flagset", path.Base(c.shared)), c.shared)
	}

	assert.Assert(t, n.IsShared("helper", n.SharedFile("helper")))
	assert.Assert(t, !n.IsShared("helper", "zz_gen.helper.kama_y.go"))

	assert.Assert(t, !constrained("d/a.go"))
	assert.Assert(t, !constrained("d/a_tagged.go"))
	assert.Assert(t, constrained("d/a_linux.go"))
	assert.Assert(t, constrained("d/a_amd64.go"))
	assert.Assert(t, constrained("d/a_test.go"))
	assert.Equal(t, n.SourceFile("d/a_test.go"), filepath.FromSlash("d/a.kama_x_test.go"))
	assert.Equal(
		t,
		n.SharedFileOf("helper", "d/a_linux_test.go"),
		filepath.FromSlash("d/zz_gen.helper.a.kama_x_linux_test.go"),
	)
	assert.Assert(t, n.IsShared("helper", "zz_gen.helper.kama_x_test.go"))
}

func TestHostShared(t *testing.T) {
	uses := func(names ...string) map[string]bool {
		m := map[string]bool{}
		for _, n := range names {
			m[n] = true
		}

		return m
	}
	file := func(src string) *ast.File {
		t.Helper()

		f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, parser.ParseComments)
		assert.NilError(t, err)

		return f
	}
	plain := file("package x\n")
	n := Naming{Name: "kama_x"}

	hosts := HostShared(n, "helper", "shared.go", []Caller{
		{Src: "p.go", File: plain, Uses: uses("everywhere", "both")},
		// A build line constrains a caller, and so does a file name suffix.
		{
			Src:  "a.go",
			File: file("//go:build a\n\npackage x\n"),
			Uses: uses("both", "onlyA", "ab"),
		},
		{Src: "b_linux.go", File: plain, Uses: uses("ab", "onlyB")},
		{Src: "q.go", File: plain, Uses: nil},
	})
	assert.DeepEqual(t, hosts, []Host{
		{Path: "shared.go", Names: []string{"both", "everywhere"}},
		// A name constrained callers share goes to the first of them.
		{
			Path:      "zz_gen.helper.a.kama_x.go",
			BuildLine: "//go:build a",
			Names:     []string{"ab", "onlyA"},
		},
		{Path: "zz_gen.helper.b.kama_x_linux.go", Names: []string{"onlyB"}},
	})
	assert.Equal(t, len(HostShared(n, "helper", "shared.go", nil)), 0)

	// A test file is constrained unless the shared file is a test file too.
	hosts = HostShared(n, "helper", "shared.go", []Caller{
		{Src: "a_test.go", File: plain, Uses: uses("onlyTest", "testAndB", "testAndP")},
		{Src: "b_linux.go", File: plain, Uses: uses("testAndB")},
		{Src: "c_test.go", File: plain, Uses: uses("onlyTest")},
		{Src: "p.go", File: plain, Uses: uses("testAndP")},
	})
	assert.DeepEqual(t, hosts, []Host{
		{Path: "shared.go", Names: []string{"testAndP"}},
		// A name a test file shares with a constrained file goes where the
		// build of the package sees it.
		{Path: "zz_gen.helper.a.kama_x_test.go", Names: []string{"onlyTest"}},
		{Path: "zz_gen.helper.b.kama_x_linux.go", Names: []string{"testAndB"}},
	})

	hosts = HostShared(n, "helper", "shared_test.go", []Caller{
		{Src: "a_test.go", File: plain, Uses: uses("a")},
		{Src: "b_linux_test.go", File: plain, Uses: uses("b")},
	})
	assert.DeepEqual(t, hosts, []Host{
		{Path: "shared_test.go", Names: []string{"a"}},
		{Path: "zz_gen.helper.b.kama_x_linux_test.go", Names: []string{"b"}},
	})
}

func TestInputRel(t *testing.T) {
	root := t.TempDir()
	in := Input[struct{}, struct{}, struct{}, struct{}]{Root: root}

	rel, err := in.Rel(filepath.Join(root, "a", "b.go"))
	assert.NilError(t, err)
	assert.Equal(t, rel, "a/b.go")

	_, err = in.Rel(filepath.Join(filepath.Dir(root), "b.go"))
	assert.ErrorContains(t, err, "is outside")
}

func TestHeader(t *testing.T) {
	hdr, err := header(
		"example.invalid/gen",
		map[string]any{"b": 1, "a": "x"},
		time.Date(2026, 11, 4, 19, 12, 33, 5, time.FixedZone("", 9*60*60)),
	)
	assert.NilError(t, err)
	assert.DeepEqual(t, hdr, []string{
		"// Code generated by example.invalid/gen. DO NOT EDIT.",
		"// params:",
		"//\ta: \"x\"",
		"//\tb: 1",
		"// generated: 2026-11-04T10:12:33Z",
	})
}

func TestStaleCheckInScope(t *testing.T) {
	gen := Id{Namespace: "acme", Name: "dirgen"}

	reg := genmark.NewRegistry(ToolName)
	assert.NilError(t, reg.Register(gen.String()))
	assert.NilError(t, reg.Register("acme:othergen"))

	ctx := context.Background()
	f, err := overlayfs.New(t.TempDir())
	assert.NilError(t, err)

	defer f.Close()

	marked := func(generator, name string) []byte {
		return []byte("package x\n\n//kama:generated " + generator + "\ntype " + name + " int\n")
	}
	for name, src := range map[string][]byte{
		"out/hand.go":      []byte("package x\n\ntype Hand int\n"),
		"out/made.go":      marked(gen.String(), "Made"),
		"out/stale.go":     marked(gen.String(), "Stale"),
		"out/foreign.go":   marked("acme:othergen", "Foreign"),
		"out/sub/below.go": marked(gen.String(), "Below"),
		"edited/x.go": []byte(
			"package x\n\n// X is x.\n//\n//kama:generated acme:dirgen\ntype X int\n",
		),
		"elsewhere/x.go": marked(gen.String(), "Elsewhere"),
	} {
		assert.NilError(t, f.Write(ctx, name, src))
	}

	check := StaleCheck{Registry: reg, Generator: gen}
	warnings, err := check.InScope(
		f.FS(),
		Scope{Files: []string{"edited/x.go", "out/made.go"}, Dirs: []string{"out"}},
		[]File{{Path: "out/made.go"}},
	)
	assert.NilError(t, err)
	assert.Equal(t, len(warnings), 2, "%v", warnings)
	assert.ErrorIs(t, warnings[0], ErrStale)
	assert.ErrorContains(t, warnings[0], "edited/x.go holds declarations generated by acme:dirgen")
	assert.ErrorContains(
		t,
		warnings[1],
		"out/stale.go holds declarations generated by acme:dirgen",
	)

	// A run producing nothing for its directory finds every marked file.
	warnings, err = check.InScope(f.FS(), Scope{Dirs: []string{"out"}}, nil)
	assert.NilError(t, err)
	assert.Equal(t, len(warnings), 2, "%v", warnings)

	// A file or a directory not yet there holds no stale file.
	warnings, err = check.InScope(
		f.FS(),
		Scope{Files: []string{"missing/a.go"}, Dirs: []string{"missing"}},
		nil,
	)
	assert.NilError(t, err)
	assert.Equal(t, len(warnings), 0)
}

func TestStaleCheckScopePaths(t *testing.T) {
	gen := Id{Namespace: "acme", Name: "dirgen"}

	reg := genmark.NewRegistry(ToolName)
	assert.NilError(t, reg.Register(gen.String()))

	f, err := overlayfs.New(t.TempDir())
	assert.NilError(t, err)

	defer f.Close()

	assert.NilError(t, f.Write(
		context.Background(),
		"out/a.go",
		[]byte("package x\n\n//kama:generated acme:dirgen\ntype A int\n"),
	))

	check := StaleCheck{Registry: reg, Generator: gen}

	warnings, err := check.InScope(f.FS(), Scope{Files: []string{"./out/a.go"}}, nil)
	assert.NilError(t, err)
	assert.Equal(t, len(warnings), 1, "%v", warnings)

	warnings, err = check.InScope(f.FS(), Scope{Dirs: []string{"./out/"}}, nil)
	assert.NilError(t, err)
	assert.Equal(t, len(warnings), 1, "%v", warnings)

	for _, scope := range []Scope{{Files: []string{"../a.go"}}, {Dirs: []string{"/out"}}} {
		_, err = check.InScope(f.FS(), scope, nil)
		assert.ErrorContains(t, err, "base: scope: invalid path")
	}
}

func TestIdProcKind(t *testing.T) {
	assert.Equal(t, Id{Namespace: "kama", Name: "partial"}.ProcKind(), "partial.generate")
	assert.Equal(t, Id{Namespace: "acme", Name: "partial"}.ProcKind(), "acme.partial.generate")
}

func TestIdValidate(t *testing.T) {
	for _, id := range []Id{
		{"kama", "partial"},
		{"acme", "foo_bar"},
		{"Acme2", "x1"},
		{"acme", "testing"},
	} {
		assert.NilError(t, id.Validate(), "%v", id)
	}

	for _, id := range []Id{
		{"", "foo"},
		{"acme", ""},
		{"acme", "dir-gen"},
		{"ac:me", "foo"},
		{"acme", "a:b"},
		{"acme", "1foo"},
		{"acme", "_foo"},
		{"acme", "foo bar"},
		{"acmé", "foo"},
		{"acme", "test"},
		{"acme", "foo_test"},
		{"acme", "linux"},
		{"acme", "amd64"},
		{"linux", "amd64"},
		{"acme", "windows_arm64"},
	} {
		assert.ErrorIs(t, id.Validate(), ErrInvalidId, "%v", id)
	}
}

func TestMustRegisterPanicsOnInvalidId(t *testing.T) {
	defer func() {
		err, _ := recover().(error)
		assert.ErrorIs(t, err, ErrInvalidId)
	}()

	MustRegister(Id{Namespace: "acme", Name: "test"})
}
