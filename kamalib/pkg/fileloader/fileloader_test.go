package fileloader

import (
	"testing"
	"testing/fstest"

	"gotest.tools/v3/assert"
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
		assert.NilError(t, err)

		assert.Equal(t, name, c.wantName)
		assert.Equal(t, d.Name, c.want)
	}
}

func TestLoadErrors(t *testing.T) {
	fsys := fstest.MapFS{}
	var d doc

	_, err := Load(fsys, PathInput{Base: "none", Formats: []Format{FormatJson}}, &d)
	assert.ErrorIs(t, err, ErrNotFound)

	_, err = Load(fsys, PathInput{Base: "none", Formats: []Format{"ini"}}, &d)
	assert.ErrorIs(t, err, ErrUnknownFormat)
}

func TestCandidates(t *testing.T) {
	got, err := PathInput{Base: "a/meta", Formats: []Format{FormatYaml, FormatJson}}.Candidates()
	assert.NilError(t, err)

	assert.DeepEqual(t, got, []string{"a/meta.yaml", "a/meta.yml", "a/meta.json"})
}
