package metadata

import (
	"errors"
	"slices"
	"testing"
	"testing/fstest"
)

type doc struct {
	Name string `json:"name" yaml:"name" toml:"name"`
}

func TestLoadPriority(t *testing.T) {
	fsys := fstest.MapFS{
		"meta.json": {Data: []byte(`{"name":"json"}`)},
		"meta.yml":  {Data: []byte("name: yaml\n")},
		"meta.toml": {Data: []byte("name = \"toml\"\n")},
	}
	cases := []struct {
		formats        []Format
		wantName, want string
	}{
		{[]Format{FormatYaml, FormatJson}, "meta.yml", "yaml"},
		{[]Format{FormatJson, FormatYaml}, "meta.json", "json"},
		{[]Format{FormatToml}, "meta.toml", "toml"},
	}
	for _, c := range cases {
		var d doc
		name, err := Load(fsys, PathInput{Base: "meta", Formats: c.formats}, &d)
		if err != nil {
			t.Fatalf("Load(%v): %v", c.formats, err)
		}
		if name != c.wantName || d.Name != c.want {
			t.Errorf("Load(%v) = %q/%q, want %q/%q", c.formats, name, d.Name, c.wantName, c.want)
		}
	}
}

func TestLoadErrors(t *testing.T) {
	fsys := fstest.MapFS{}
	var d doc
	_, err := Load(fsys, PathInput{Base: "none", Formats: []Format{FormatJson}}, &d)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("missing: got %v, want ErrNotFound", err)
	}
	_, err = Load(fsys, PathInput{Base: "none", Formats: []Format{"ini"}}, &d)
	if !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("unknown format: got %v, want ErrUnknownFormat", err)
	}
}

func TestCandidates(t *testing.T) {
	got, err := PathInput{Base: "a/meta", Formats: []Format{FormatYaml, FormatJson}}.Candidates()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a/meta.yaml", "a/meta.yml", "a/meta.json"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
