package metadata

import (
	"errors"
	"testing"
	"testing/fstest"
)

type doc struct {
	Name string `json:"name" yaml:"name" toml:"name"`
}

func TestLoadPriority(t *testing.T) {
	fsys := fstest.MapFS{
		"meta.json": {Data: []byte(`{"name":"json"}`)},
		"meta.yaml": {Data: []byte("name: yaml\n")},
		"meta.toml": {Data: []byte("name = \"toml\"\n")},
	}
	yamlFirst := []Candidate{{Name: "meta.yaml"}, {Name: "meta.json"}}
	jsonFirst := []Candidate{{Name: "meta.json"}, {Name: "meta.yaml"}}

	cases := []struct {
		candidates     []Candidate
		wantName, want string
	}{
		{yamlFirst, "meta.yaml", "yaml"},
		{jsonFirst, "meta.json", "json"},
		{[]Candidate{{Name: "missing.yaml"}, {Name: "meta.toml"}}, "meta.toml", "toml"},
		{[]Candidate{{Name: "meta.json", Format: FormatJSON}}, "meta.json", "json"},
	}
	for _, c := range cases {
		var d doc
		name, err := Load(fsys, c.candidates, &d)
		if err != nil {
			t.Fatalf("Load(%v): %v", c.candidates, err)
		}
		if name != c.wantName || d.Name != c.want {
			t.Errorf("Load(%v) = %q/%q, want %q/%q", c.candidates, name, d.Name, c.wantName, c.want)
		}
	}
}

func TestLoadErrors(t *testing.T) {
	fsys := fstest.MapFS{"meta.ini": {Data: []byte("x")}}
	var d doc
	if _, err := Load(fsys, []Candidate{{Name: "none.json"}}, &d); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing: got %v, want ErrNotFound", err)
	}
	_, err := Load(fsys, []Candidate{{Name: "meta.ini"}}, &d)
	if !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("unknown ext: got %v, want ErrUnknownFormat", err)
	}
}
