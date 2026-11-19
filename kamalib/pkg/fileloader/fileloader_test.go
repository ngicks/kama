package fileloader

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"strings"
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

func TestFormatOf(t *testing.T) {
	cases := []struct {
		name string
		want Format
		ok   bool
	}{
		{"a/meta.json", FormatJson, true},
		{"meta.yaml", FormatYaml, true},
		{"meta.yml", FormatYaml, true},
		{"meta.toml", FormatToml, true},
		{"meta.ini", "", false},
		{"meta", "", false},
		{"meta.YAML", "", false},
	}

	for _, c := range cases {
		got, ok := FormatOf(c.name)
		assert.Equal(t, got, c.want, c.name)
		assert.Equal(t, ok, c.ok, c.name)
	}
}

func TestLoadFile(t *testing.T) {
	fsys := fstest.MapFS{
		"a/meta.yml": {Data: []byte("name: yaml\n")},
		"meta.ini":   {Data: []byte("name = ini\n")},
		"bad.json":   {Data: []byte("{")},
	}

	var d doc
	assert.NilError(t, LoadFile(fsys, "a/meta.yml", &d))
	assert.Equal(t, d.Name, "yaml")

	assert.ErrorIs(t, LoadFile(fsys, "meta.ini", &d), ErrUnknownFormat)
	assert.ErrorIs(t, LoadFile(fsys, "none.json", &d), fs.ErrNotExist)
	assert.ErrorContains(t, LoadFile(fsys, "bad.json", &d), `fileloader: decode "bad.json"`)
}

func TestCandidates(t *testing.T) {
	got, err := PathInput{Base: "a/meta", Formats: []Format{FormatYaml, FormatJson}}.Candidates()
	assert.NilError(t, err)

	assert.DeepEqual(t, got, []string{"a/meta.yaml", "a/meta.yml", "a/meta.json"})
}

func TestDecodeJson(t *testing.T) {
	var d doc
	assert.NilError(
		t,
		Decode(strings.NewReader("{\"name\":\"a\",\"extra\":1}\n\n"), FormatJson, &d),
	)
	assert.Equal(t, d.Name, "a")

	var cased doc
	assert.NilError(t, Decode(strings.NewReader(`{"Name":"a"}`), FormatJson, &cased))
	assert.Equal(t, cased.Name, "")

	var data any
	assert.NilError(t, Decode(strings.NewReader(`{"a":[1,"s",true,null,{}]}`), FormatJson, &data))
	assert.DeepEqual(t, data, map[string]any{"a": []any{1.0, "s", true, nil, map[string]any{}}})
}

func TestDecodeJsonErrors(t *testing.T) {
	cases := []struct {
		name, src string
	}{
		{"trailing data", `{"name":"a"} {"name":"b"}`},
		{"trailing garbage", `{"name":"a"}x`},
		{"empty", ""},
		{"duplicate name", `{"name":"a","name":"b"}`},
		{"invalid UTF-8", "{\"name\":\"\xff\"}"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var data any

			err := Decode(strings.NewReader(c.src), FormatJson, &data)

			var synErr *jsontext.SyntacticError
			assert.Assert(t, errors.As(err, &synErr), "%v", err)
		})
	}
}

func TestPathInputJson(t *testing.T) {
	var in PathInput
	assert.NilError(t, json.Unmarshal([]byte(`{"base":"a/meta","formats":["yaml","json"]}`), &in))
	assert.DeepEqual(t, in, PathInput{Base: "a/meta", Formats: []Format{FormatYaml, FormatJson}})
}
