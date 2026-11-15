package suffix

import (
	"os/exec"
	"slices"
	"testing"

	"gotest.tools/v3/assert"
)

func TestSuffixFilename(t *testing.T) {
	for _, set := range [][2]string{
		{"foo.go", "foo.suffix.go"},
		{"foo_test.go", "foo.suffix_test.go"},
		{"foo_linux.go", "foo.suffix_linux.go"},
		{"foo_amd64.go", "foo.suffix_amd64.go"},
		{"foo_linux_amd64.go", "foo.suffix_linux_amd64.go"},
		{"foo_linux_amd64_test.go", "foo.suffix_linux_amd64_test.go"},
		{"foo_amd64_linux.go", "foo_amd64.suffix_linux.go"}, // wrong
		{"foo_bar_linux.go", "foo_bar.suffix_linux.go"},
		{"foo_bar.go", "foo_bar.suffix.go"},
	} {
		assert.Equal(t, set[1], SuffixFilename(set[0], ".suffix"))
	}
}

func TestIsSuffixed(t *testing.T) {
	for _, name := range []string{
		"foo.suffix.go",
		"foo.suffix_test.go",
		"foo.suffix_linux.go",
		"foo.suffix_amd64.go",
		"foo.suffix_linux_amd64.go",
		"foo.suffix_linux_amd64_test.go",
		"foo_amd64.suffix_linux.go",
		"foo_bar.suffix_linux.go",
		"foo_bar.suffix.go",
	} {
		assert.Assert(t, IsSuffixed(name, ".suffix"))
	}

	for _, name := range []string{
		"foo.go",
		"foo.suffix_testa.go",
		"foo.suffix_linuxa.go",
		"foo.suffix_amd64d.go",
		"foo.suffix_amd64_linux.go",
	} {
		assert.Assert(t, !IsSuffixed(name, ".suffix"))
	}
}

func TestListDist(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not on PATH")
	}

	got, err := listDist()
	assert.NilError(t, err)

	assert.Assert(t, slices.ContainsFunc(got, func(l goToolDistListJson) bool {
		return l.GOOS == "linux" && l.GOARCH == "amd64" && l.CgoSupported && l.FirstClass
	}))
	for _, l := range got {
		assert.Assert(t, l.GOOS != "" && l.GOARCH != "", "%+v", l)
	}
}
