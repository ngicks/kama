// Package fileloader decodes structured metadata files in JSON, YAML or TOML.
//
// An Input finds the files to load. PathInput names one base path and the
// formats it accepts, in priority order, and finds the first format whose
// file is present. JSON files are decoded with encoding/json/v2.
package fileloader

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"path"
	"slices"

	"github.com/ngicks/opiter"
	"github.com/pelletier/go-toml/v2"
	"go.yaml.in/yaml/v4"
)

// Format selects a decoder.
type Format string

const (
	FormatJson Format = "json"
	FormatYaml Format = "yaml"
	FormatToml Format = "toml"
)

var (
	ErrUnknownFormat = errors.New("fileloader: unknown format")
	ErrNotFound      = errors.New("fileloader: no file found")
)

// Extensions returns the file extensions a format is read from, including the
// dot, in the order they are probed.
func (f Format) Extensions() []string {
	switch f {
	case FormatJson:
		return []string{".json"}
	case FormatYaml:
		return []string{".yaml", ".yml"}
	case FormatToml:
		return []string{".toml"}
	default:
		return nil
	}
}

// FormatOf returns the format whose Extensions hold the extension of name.
func FormatOf(name string) (Format, bool) {
	ext := path.Ext(name)

	return opiter.WrapSeq(slices.Values([]Format{FormatJson, FormatYaml, FormatToml})).
		Filter(func(f Format) bool { return slices.Contains(f.Extensions(), ext) }).
		First().
		Unpack()
}

// Input finds metadata files.
type Input interface {
	// Find returns the names of the files to load, slash-separated and
	// relative to the root of fsys. It returns no name when no file is
	// present.
	Find(fsys fs.FS) ([]string, error)
}

// Document is a file LoadAll decoded.
type Document struct {
	// Name is the name of the file as Input.Find returned it.
	Name string
	// Data is the document decoded into an any.
	Data any
}

// LoadAll decodes every file in finds in fsys, sorted by name in byte
// order. The extension of each name gives its format, as FormatOf does.
func LoadAll(fsys fs.FS, in Input) ([]Document, error) {
	names, err := in.Find(fsys)
	if err != nil {
		return nil, err
	}

	names = slices.Sorted(slices.Values(names))
	docs := make([]Document, 0, len(names))

	for _, name := range names {
		var data any
		if err := LoadFile(fsys, name, &data); err != nil {
			return nil, err
		}

		docs = append(docs, Document{Name: name, Data: data})
	}

	return docs, nil
}

// PathInput names a metadata file without its extension and the formats it
// may be written in. Its keys in a workflow spec are base and formats.
type PathInput struct {
	// Base is the path relative to the fs passed to Load, without extension.
	Base string `json:"base"`
	// Formats are tried in order; earlier entries take priority.
	Formats []Format `json:"formats"`
	// Unknown captures the keys of its spec entry that name no field.
	Unknown map[string]any `json:",embed"`
}

// Candidates lists every file name Load probes, in order.
func (p PathInput) Candidates() ([]string, error) {
	unknown := opiter.WrapSeq(slices.Values(p.Formats)).
		Filter(func(f Format) bool { return f.Extensions() == nil }).
		First()

	if unknown.IsSome() {
		return nil, fmt.Errorf("%w: %q", ErrUnknownFormat, unknown.Value())
	}

	return opiter.WrapSeq(slices.Values(p.Formats)).
		FlatMap(func(f Format) iter.Seq[string] { return slices.Values(f.Extensions()) }).
		Map(func(ext string) string { return p.Base + ext }).
		CollectSlice(), nil
}

// Find returns the first candidate of p present in fsys, or no name when
// none is present. It implements Input.
func (p PathInput) Find(fsys fs.FS) ([]string, error) {
	candidates, err := p.Candidates()
	if err != nil {
		return nil, err
	}

	for _, name := range candidates {
		_, err := fs.Stat(fsys, name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}

		if err != nil {
			return nil, fmt.Errorf("fileloader: stat %q: %w", name, err)
		}

		return []string{name}, nil
	}

	return nil, nil
}

// Decode decodes one document from r into v.
//
// JSON is decoded with encoding/json/v2. The input must hold exactly one JSON
// value. Data after the value other than whitespace is an error. An empty
// input, a duplicate object name and invalid UTF-8 are errors too. Object
// names match struct fields case-sensitively. Unknown names are ignored. JSON
// decoded into an any yields map[string]any, []any, string, float64, bool and
// nil.
func Decode(r io.Reader, format Format, v any) error {
	switch format {
	case FormatJson:
		return json.UnmarshalRead(r, v)
	case FormatYaml:
		return yaml.NewDecoder(r).Decode(v)
	case FormatToml:
		return toml.NewDecoder(r).Decode(v)
	default:
		return fmt.Errorf("%w: %q", ErrUnknownFormat, format)
	}
}

// Load decodes the first file of in that exists in fsys into v and returns
// its name. A missing file moves on to the next; any other error stops.
func Load(fsys fs.FS, in PathInput, v any) (string, error) {
	for _, format := range in.Formats {
		exts := format.Extensions()
		if exts == nil {
			return "", fmt.Errorf("%w: %q", ErrUnknownFormat, format)
		}

		for _, ext := range exts {
			name := in.Base + ext

			err := decodeFile(fsys, name, format, v)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}

			if err != nil {
				return "", err
			}

			return name, nil
		}
	}

	return "", fmt.Errorf("%w: %q", ErrNotFound, in.Base)
}

// LoadFile decodes the file name of fsys into v, in the format FormatOf
// gives for name. An extension of no format is an error wrapping
// ErrUnknownFormat, and a missing file an error wrapping fs.ErrNotExist.
func LoadFile(fsys fs.FS, name string, v any) error {
	format, ok := FormatOf(name)
	if !ok {
		return fmt.Errorf("%w: extension of %q", ErrUnknownFormat, name)
	}

	return decodeFile(fsys, name, format, v)
}

func decodeFile(fsys fs.FS, name string, format Format, v any) error {
	f, err := fsys.Open(name)
	if err != nil {
		return fmt.Errorf("fileloader: open %q: %w", name, err)
	}
	defer f.Close()

	if err := Decode(f, format, v); err != nil {
		return fmt.Errorf("fileloader: decode %q: %w", name, err)
	}

	return nil
}
