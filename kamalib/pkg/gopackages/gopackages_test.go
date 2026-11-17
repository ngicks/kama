package gopackages

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ngicks/opiter"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
)

func TestLoadSeesStagedFiles(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	assert.NilError(
		t,
		os.WriteFile(
			filepath.Join(root, "go.mod"),
			[]byte("module example.com/x\n\ngo 1.27\n"),
			0o644,
		),
	)
	assert.NilError(
		t,
		os.WriteFile(
			filepath.Join(root, "a.go"),
			[]byte("package x\n\nfunc A() int { return B() }\n"),
			0o644,
		),
	)

	f, err := overlayfs.New(root)
	assert.NilError(t, err)

	defer f.Close()

	// B exists only in the staged layer; type checking a.go needs it.
	assert.NilError(t, f.Write(ctx, "b.gen.go", []byte("package x\n\nfunc B() int { return 1 }\n")))

	pkgs, err := Load(ctx, f, Options{Patterns: []string{"./..."}})
	assert.NilError(t, err)

	assert.Equal(t, len(pkgs), 1)
	pkg := pkgs[0]

	files := slices.Sorted(
		opiter.WrapSeq(
			slices.Values(pkg.GoFiles),
		).
			Map(filepath.Base).
			Iter(),
	)

	assert.DeepEqual(t, files, []string{"a.go", "b.gen.go"})
}

func TestLoadFailsOnPackageErrors(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		opts  Options
		// want maps a package path to one message substring per error.
		want map[string][]string
	}{
		{
			name:  "type error",
			files: map[string]string{"a.go": "package x\n\nfunc A() int { return B() }\n"},
			opts:  Options{Patterns: []string{"./..."}},
			want:  map[string][]string{"example.com/x": {"undefined: B"}},
		},
		{
			name: "errors in several packages",
			files: map[string]string{
				"a/a.go": "package a\n\nfunc A() int { return B() }\n",
				"b/b.go": "package b\n\nvar C int = \"c\"\n",
			},
			opts: Options{Patterns: []string{"./..."}},
			want: map[string][]string{
				"example.com/x/a": {"undefined: B"},
				"example.com/x/b": {"cannot use \"c\""},
			},
		},
		{
			name: "errors in a dependency",
			files: map[string]string{
				"a/a.go": "package a\n\nimport _ \"example.com/x/b\"\n",
				"b/b.go": "package b\n\nfunc B() int { return C() }\n",
			},
			opts: Options{Patterns: []string{"./a"}},
			want: map[string][]string{"example.com/x/b": {"undefined: C"}},
		},
		{
			name: "test variants report once",
			files: map[string]string{
				"a.go":      "package x\n\nfunc A() int { return B() }\n",
				"a_test.go": "package x\n",
			},
			opts: Options{Patterns: []string{"./..."}, Tests: true},
			want: map[string][]string{"example.com/x": {"undefined: B"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()

			tc.files["go.mod"] = "module example.com/x\n\ngo 1.27\n"
			for name, src := range tc.files {
				p := filepath.Join(root, filepath.FromSlash(name))
				assert.NilError(t, os.MkdirAll(filepath.Dir(p), 0o755))
				assert.NilError(t, os.WriteFile(p, []byte(src), 0o644))
			}

			f, err := overlayfs.New(root)
			assert.NilError(t, err)

			defer f.Close()

			pkgs, err := Load(ctx, f, tc.opts)
			assert.Assert(t, pkgs == nil)

			var pkgErrs PackageErrors
			assert.Assert(t, errors.As(err, &pkgErrs), "err: %v", err)
			assert.DeepEqual(
				t,
				slices.Sorted(maps.Keys(pkgErrs)),
				slices.Sorted(maps.Keys(tc.want)),
			)

			for path, msgs := range tc.want {
				got := pkgErrs[path]
				assert.Equal(t, len(got), len(msgs), "%s: %v", path, got)

				for i, msg := range msgs {
					assert.Assert(t, strings.Contains(got[i].Msg, msg), "%s: %v", path, got[i])
				}
			}
		})
	}
}

func TestLoadFailsOnMissingPattern(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	assert.NilError(
		t,
		os.WriteFile(
			filepath.Join(root, "go.mod"),
			[]byte("module example.com/x\n\ngo 1.27\n"),
			0o644,
		),
	)

	f, err := overlayfs.New(root)
	assert.NilError(t, err)

	defer f.Close()

	pkgs, err := Load(ctx, f, Options{Patterns: []string{"./nonexistent"}})
	assert.Assert(t, pkgs == nil)

	var pkgErrs PackageErrors
	assert.Assert(t, errors.As(err, &pkgErrs), "err: %v", err)
	assert.Assert(t, len(pkgErrs) > 0)
}

func TestPackageErrorsError(t *testing.T) {
	err := PackageErrors{
		"example.com/x/b": {{Pos: "b.go:1:1", Msg: "bad b"}},
		"example.com/x/a": {{Pos: "a.go:1:1", Msg: "bad a1"}, {Msg: "bad a2"}},
	}
	assert.Equal(
		t,
		err.Error(),
		"example.com/x/a:\n\ta.go:1:1: bad a1\n\t-: bad a2\nexample.com/x/b:\n\tb.go:1:1: bad b",
	)
}
