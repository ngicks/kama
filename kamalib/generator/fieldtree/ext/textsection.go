package ext

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"go/types"
	"maps"
	"slices"
	"strings"
	"text/template"

	"github.com/ngicks/kama/kamalib/generator/genext"
	"github.com/ngicks/kama/kamalib/pkg/unknownkey"
)

// TextSectionName is the key of the section of fieldtree in the
// description of a type in a text extension file.
const TextSectionName = "fieldtree"

func init() { genext.RegisterTextSection(TextSectionName, buildTextSection) }

// TextSection is the section of fieldtree in the description of a type in a
// text extension file. It answers Reacher:
//
//	generator:
//	  fieldtree:
//	    reach:
//	      t:
//	        access: "{{.V}}.Get"
//	        note: "set only when {{.V}}.Ok"
//
// The templates are text/template, executed with the template expression
// of the value as {{.V}}. They take no function of the text extension
// file: they spell a template expression, not Go code.
type TextSection struct {
	// Reach maps a role of the type to how a template reaches its value.
	Reach   map[string]TextReach `json:"reach"`
	Unknown map[string]any       `json:",embed"`
}

// TextReach is the answer of Reacher for one role.
type TextReach struct {
	// Access is the template of the template expression of the value.
	Access string `json:"access"`
	// Note is the template of what the reader must know about the value.
	Note string `json:"note"`
	// Key is the template of what the value adds to the key path.
	Key     string         `json:"key"`
	Unknown map[string]any `json:",embed"`
}

func (s *TextSection) unknownKeys() [][]string {
	paths := unknownkey.Paths(s.Unknown)
	for _, role := range slices.Sorted(maps.Keys(s.Reach)) {
		paths = append(paths, unknownkey.Paths(s.Reach[role].Unknown, "reach", role)...)
	}

	return paths
}

func buildTextSection(
	scope genext.TextScope,
	raw jsontext.Value,
	path []string,
) (genext.TypeSupport, []error, error) {
	var sec TextSection

	warnings, err := genext.DecodeTextSection(scope, raw, path, &sec, sec.unknownKeys)
	if err != nil {
		return nil, nil, err
	}

	var errs []error

	for _, role := range slices.Sorted(maps.Keys(sec.Reach)) {
		r := sec.Reach[role]
		for _, kt := range [][2]string{{"access", r.Access}, {"note", r.Note}, {"key", r.Key}} {
			key, text := kt[0], kt[1]

			at := slices.Concat(path, []string{"reach", role, key})
			if key == "access" && strings.TrimSpace(text) == "" {
				errs = append(errs, scope.Error(at, "is required"))
				continue
			}

			if _, err := parseReach(text); err != nil {
				errs = append(errs, scope.Error(at, "%v", err))
			}
		}
	}

	if err := errors.Join(errs...); err != nil {
		return nil, warnings, err
	}

	return textReacher{scope: scope}, warnings, nil
}

func parseReach(text string) (*template.Template, error) {
	return template.New("").Option("missingkey=error").Parse(text)
}

// textReacher answers Reacher for a type a text extension file describes.
// It decodes its section from the file each time it is asked.
type textReacher struct {
	scope genext.TextScope
}

var _ Reacher = textReacher{}

func (s textReacher) Name() string {
	return s.scope.File() + "#" + s.scope.Key() + "#" + TextSectionName
}

func (s textReacher) Reach(t types.Type, role, v string) (string, string, string, bool) {
	if !s.scope.Claims(t) {
		return "", "", "", false
	}

	var sec TextSection

	_ = json.Unmarshal(s.scope.Section(TextSectionName), &sec)

	r, ok := sec.Reach[role]
	if !ok {
		return "", "", "", false
	}

	access, err1 := executeReach(r.Access, v)
	note, err2 := executeReach(r.Note, v)
	key, err3 := executeReach(r.Key, v)

	// The templates parsed when the file loaded, and .V is their only data,
	// so an error here is one of execution, which the line cannot show.
	if errors.Join(err1, err2, err3) != nil {
		return "", "", "", false
	}

	return access, note, key, true
}

func executeReach(text, v string) (string, error) {
	tmpl, err := parseReach(text)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	if err := tmpl.Execute(&b, map[string]string{"V": v}); err != nil {
		return "", err
	}

	return b.String(), nil
}
