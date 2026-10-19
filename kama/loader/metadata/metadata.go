// Package metadata decodes structured metadata files in JSON, YAML or TOML.
//
// Callers name one base path and the formats they accept, in priority order.
// The first format whose file is present is decoded.
package metadata

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"

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
	ErrUnknownFormat = errors.New("metadata: unknown format")
	ErrNotFound      = errors.New("metadata: no file found")
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

// PathInput names a metadata file without its extension and the formats it
// may be written in.
type PathInput struct {
	// Base is the path relative to the fs passed to Load, without extension.
	Base string
	// Formats are tried in order; earlier entries take priority.
	Formats []Format
}

// Candidates lists every file name Load probes, in order.
func (p PathInput) Candidates() ([]string, error) {
	var names []string
	for _, f := range p.Formats {
		exts := f.Extensions()
		if exts == nil {
			return nil, fmt.Errorf("%w: %q", ErrUnknownFormat, f)
		}
		for _, ext := range exts {
			names = append(names, p.Base+ext)
		}
	}
	return names, nil
}

// Decode decodes one document from r into v.
func Decode(r io.Reader, format Format, v any) error {
	switch format {
	case FormatJson:
		return json.NewDecoder(r).Decode(v)
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
			f, err := fsys.Open(name)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					continue
				}
				return "", fmt.Errorf("metadata: open %q: %w", name, err)
			}
			err = Decode(f, format, v)
			f.Close()
			if err != nil {
				return "", fmt.Errorf("metadata: decode %q: %w", name, err)
			}
			return name, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrNotFound, in.Base)
}
