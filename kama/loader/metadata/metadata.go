// Package metadata decodes structured metadata files in JSON, YAML or TOML.
//
// Callers name the files they accept, in priority order, and the first one
// present is decoded. The format is given explicitly or inferred from the
// file extension.
package metadata

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"go.yaml.in/yaml/v4"
)

// Format selects a decoder.
type Format string

const (
	// FormatAuto infers the format from the file extension.
	FormatAuto Format = ""
	FormatJSON Format = "json"
	FormatYAML Format = "yaml"
	FormatTOML Format = "toml"
)

var (
	ErrUnknownFormat = errors.New("metadata: unknown format")
	ErrNotFound      = errors.New("metadata: no candidate file found")
)

// Candidate is one file the caller accepts.
type Candidate struct {
	// Name is the path relative to the fs passed to Load.
	Name string
	// Format overrides extension-based detection when set.
	Format Format
}

// FormatOf infers the format from name's extension.
func FormatOf(name string) (Format, error) {
	switch strings.ToLower(path.Ext(name)) {
	case ".json":
		return FormatJSON, nil
	case ".yaml", ".yml":
		return FormatYAML, nil
	case ".toml":
		return FormatTOML, nil
	default:
		return FormatAuto, fmt.Errorf("%w: %q", ErrUnknownFormat, name)
	}
}

// Decode decodes one document from r into v.
func Decode(r io.Reader, format Format, v any) error {
	switch format {
	case FormatJSON:
		return json.NewDecoder(r).Decode(v)
	case FormatYAML:
		return yaml.NewDecoder(r).Decode(v)
	case FormatTOML:
		return toml.NewDecoder(r).Decode(v)
	default:
		return fmt.Errorf("%w: %q", ErrUnknownFormat, format)
	}
}

// Load decodes the first candidate that exists in fsys into v and returns
// its name. Candidates are tried in order, so earlier entries take priority.
// A missing file moves on to the next candidate; any other error stops.
func Load(fsys fs.FS, candidates []Candidate, v any) (string, error) {
	for _, c := range candidates {
		format := c.Format
		if format == FormatAuto {
			var err error
			format, err = FormatOf(c.Name)
			if err != nil {
				return "", err
			}
		}
		f, err := fsys.Open(c.Name)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return "", fmt.Errorf("metadata: open %q: %w", c.Name, err)
		}
		err = Decode(f, format, v)
		f.Close()
		if err != nil {
			return "", fmt.Errorf("metadata: decode %q: %w", c.Name, err)
		}
		return c.Name, nil
	}
	return "", ErrNotFound
}
