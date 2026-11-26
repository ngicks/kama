package genext

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"go/token"
	"maps"
	"slices"
	"strconv"
	"strings"
	"text/template"

	"github.com/ngicks/kama/kamalib/pkg/unknownkey"
	"github.com/ngicks/opiter"
)

// TextFileVersion is the only version of the format of text extension
// files.
const TextFileVersion = 1

// ErrTextFile marks an error of a text extension file: a document that does
// not decode, a template that does not parse or execute, and a value that
// names something the file does not declare.
var ErrTextFile = errors.New("genext: invalid text extension file")

// TextDocument is a text extension file a loader read.
type TextDocument struct {
	// Name names the file in errors and warnings.
	Name string
	// Data is the document decoded into an any, as fileloader decodes it.
	Data any
}

// TextFile is the decoded form of a text extension file. The file is the
// one source of the type supports it describes: a support built from it
// keeps the file and executes its templates each time it is asked.
type TextFile struct {
	// Version is TextFileVersion.
	Version int `json:"version"`
	// Imports maps the names templates refer to packages by, through the
	// function pkg, to the import paths of packages outside the standard
	// library.
	Imports map[string]string `json:"imports"`
	// Helpers are the helper declarations templates call through the
	// function helper, by name.
	Helpers map[string]TextHelper `json:"helpers"`
	// Types describe the types the file claims, keyed by the import path of
	// the package and the name of the type joined by a dot, as
	// "example.com/deque.Deque".
	Types map[string]TextType `json:"types"`
	// Unknown captures the keys that name no field.
	Unknown map[string]any `json:",embed"`
}

// TextHelper is a helper declaration of a text extension file.
type TextHelper struct {
	// Decl is the template of exactly one package-level function, variable
	// or constant declaration named {{.Name}}.
	Decl    string         `json:"decl"`
	Unknown map[string]any `json:",embed"`
}

// TextType describes one named type.
type TextType struct {
	// Holder describes the one value the type may hold.
	Holder *TextHolder `json:"holder"`
	// Container describes the elements of the type.
	Container *TextContainer `json:"container"`
	// Composite describes the values the type holds side by side.
	Composite *TextComposite `json:"composite"`
	// Generator holds a section per generator, keyed by the name of the
	// generator, which the section builder registered under that name
	// decodes.
	Generator map[string]jsontext.Value `json:"generator"`
	Unknown   map[string]any            `json:",embed"`
}

// TextHolder describes a Holder.
type TextHolder struct {
	// Role names the held value.
	Role string `json:"role"`
	// Type is the type of the held value, as TextValueType spells it.
	Type string `json:"type"`
	// Guard is the template of the condition of the value holding a value.
	Guard string `json:"guard"`
	// Value is the template of the held value.
	Value   string         `json:"value"`
	Unknown map[string]any `json:",embed"`
}

// TextContainer describes a Container.
type TextContainer struct {
	// Role names an element.
	Role string `json:"role"`
	// Type is the type of an element, as TextValueType spells it.
	Type string `json:"type"`
	// Over is the template of the expression a range statement ranges over.
	Over string `json:"over"`
	// Yields is "values" when Over yields the elements alone, as an
	// iter.Seq does, and "pairs" when it yields an index or a key and the
	// element, as an iter.Seq2 does.
	Yields string `json:"yields"`
	// Segment is "index" or "key", what the first value of a pair locates an
	// element by. It is "index" for "values", which counts the elements.
	Segment string         `json:"segment"`
	Unknown map[string]any `json:",embed"`
}

// TextComposite describes a Composite.
type TextComposite struct {
	// Parts are the values in the order a generator walks them.
	Parts   []TextPart     `json:"parts"`
	Unknown map[string]any `json:",embed"`
}

// TextPart is one value of a Composite.
type TextPart struct {
	// Role names the value.
	Role string `json:"role"`
	// Type is the type of the value, as TextValueType spells it.
	Type string `json:"type"`
	// Guard is the template of the condition of holding the value. Empty
	// stands for always.
	Guard string `json:"guard"`
	// Value is the template of the value.
	Value string `json:"value"`
	// Segment is "none", "index", "key" or "field". Empty stands for none.
	Segment string `json:"segment"`
	// Label is the template of the label of the segment, a Go expression,
	// as "\"left\"" for a field.
	Label   string         `json:"label"`
	Unknown map[string]any `json:",embed"`
}

// TextSection builds the type support of the section of one generator in
// the description of a type. s answers through the type and the templates
// of the file. path locates raw below the entry of the type, as
// generator.validate, for TextScope.Check, TextScope.Error and
// TextScope.Gen. warnings name the keys of raw the section does not know.
type TextSection func(s TextScope, raw jsontext.Value, path []string) (
	support TypeSupport,
	warnings []error,
	err error,
)

var textSections = map[string]TextSection{}

// RegisterTextSection registers build as the builder of the sections
// keyed by the generator name. A generator package calls it in its init
// function. It panics on a name registered already.
func RegisterTextSection(name string, build TextSection) {
	if _, ok := textSections[name]; ok {
		panic(fmt.Errorf("genext: text section %s is registered twice", name))
	}

	textSections[name] = build
}

// LoadText returns the type supports the text extension files docs
// describe, in the order of docs and of the type keys, and the warnings of
// the keys the files hold that name nothing. Two files describing one
// type, or declaring one helper, are an error.
func LoadText(docs []TextDocument) ([]TypeSupport, []error, error) {
	var (
		supports []TypeSupport
		warnings []error
		errs     []error
	)

	typeFiles := map[string]string{}
	helperFiles := map[string]string{}

	for _, doc := range docs {
		f, err := decodeTextFile(doc)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		warnings = append(warnings, textUnknown(doc.Name, f)...)

		for _, name := range slices.Sorted(maps.Keys(f.Helpers)) {
			if prev, ok := helperFiles[name]; ok {
				errs = append(errs, textError(doc.Name, []string{"helpers", name},
					"helper %s is declared in %s too", name, prev))
			}

			helperFiles[name] = doc.Name
		}

		for _, key := range slices.Sorted(maps.Keys(f.Types)) {
			if prev, ok := typeFiles[key]; ok {
				errs = append(errs, textError(doc.Name, []string{"types", key},
					"type %s is described in %s too", key, prev))
			}

			typeFiles[key] = doc.Name
		}

		errs = append(errs, checkText(doc.Name, f)...)

		for _, key := range slices.Sorted(maps.Keys(f.Types)) {
			s := TextScope{file: doc.Name, doc: f, key: key}
			supports = append(supports, textSupport{scope: s})

			for _, gen := range slices.Sorted(maps.Keys(f.Types[key].Generator)) {
				path := []string{"generator", gen}

				build, ok := textSections[gen]
				if !ok {
					warnings = append(warnings, textWarning(doc.Name, s.path(path...),
						"no generator of this program is named %s", gen))

					continue
				}

				support, ws, err := build(s, f.Types[key].Generator[gen], path)
				warnings = append(warnings, ws...)

				if err != nil {
					errs = append(errs, err)
					continue
				}

				supports = append(supports, support)
			}
		}
	}

	return supports, warnings, errors.Join(errs...)
}

func decodeTextFile(doc TextDocument) (*TextFile, error) {
	data, err := normalizeText(doc.Data)
	if err != nil {
		return nil, textError(doc.Name, nil, "%v", err)
	}

	// The document passes through JSON so that the json tags and the
	// embedded fallback fields apply to every format alike.
	b, err := json.Marshal(data)
	if err != nil {
		return nil, textError(doc.Name, nil, "%v", err)
	}

	var f TextFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, textError(doc.Name, nil, "%v", err)
	}

	if f.Version != TextFileVersion {
		return nil, textError(doc.Name, []string{"version"},
			"unsupported version %d; want %d", f.Version, TextFileVersion)
	}

	return &f, nil
}

// DecodeTextSection decodes raw, the section at path of the file of s, into
// v, a pointer to a struct, and returns the warnings of the members
// unknown returns for v after decoding.
func DecodeTextSection(
	s TextScope,
	raw jsontext.Value,
	path []string,
	v any,
	unknown func() [][]string,
) ([]error, error) {
	if err := json.Unmarshal(raw, v); err != nil {
		return nil, s.Error(path, "%v", err)
	}

	return textWarnings(s.file, s.path(path...), unknown()), nil
}

// normalizeText returns v with the tables YAML decodes as map[any]any as
// map[string]any, which JSON encodes. A key that is no string is an error.
func normalizeText(v any) (any, error) {
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			n, err := normalizeText(e)
			if err != nil {
				return nil, err
			}

			out[k] = n
		}

		return out, nil
	case map[any]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			s, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("key %v is not a string", k)
			}

			n, err := normalizeText(e)
			if err != nil {
				return nil, err
			}

			out[s] = n
		}

		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			n, err := normalizeText(e)
			if err != nil {
				return nil, err
			}

			out[i] = n
		}

		return out, nil
	}

	return v, nil
}

func textUnknown(name string, f *TextFile) []error {
	paths := unknownkey.Paths(f.Unknown)

	for _, n := range slices.Sorted(maps.Keys(f.Helpers)) {
		paths = append(paths, unknownkey.Paths(f.Helpers[n].Unknown, "helpers", n)...)
	}

	for _, key := range slices.Sorted(maps.Keys(f.Types)) {
		t := f.Types[key]
		at := []string{"types", key}
		paths = append(paths, unknownkey.Paths(t.Unknown, at...)...)

		if t.Holder != nil {
			paths = append(paths, unknownkey.Paths(t.Holder.Unknown, append(at, "holder")...)...)
		}

		if t.Container != nil {
			paths = append(
				paths,
				unknownkey.Paths(t.Container.Unknown, append(at, "container")...)...,
			)
		}

		if t.Composite != nil {
			paths = append(
				paths,
				unknownkey.Paths(t.Composite.Unknown, append(at, "composite")...)...,
			)

			for i, p := range t.Composite.Parts {
				paths = append(paths, unknownkey.Paths(
					p.Unknown, append(at, "composite", "parts", fmt.Sprint(i))...,
				)...)
			}
		}
	}

	return textWarnings(name, nil, paths)
}

func textWarnings(name string, prefix []string, paths [][]string) []error {
	return opiter.SlicesValues(paths).
		Map(func(p []string) error {
			return textWarning(name, slices.Concat(prefix, p), "unknown key")
		}).
		CollectSlice()
}

// checkText reports the values of f that are wrong whatever type a run
// meets: an import name or a role that is no identifier, a helper name
// that is no identifier, a word of a closed set that is none of them, and
// a template that does not parse.
func checkText(name string, f *TextFile) []error {
	var errs []error

	fail := func(path []string, format string, args ...any) {
		errs = append(errs, textError(name, path, format, args...))
	}

	parse := func(path []string, text string) {
		if _, err := parseText(text); err != nil {
			fail(path, "%v", err)
		}
	}

	// A template left out would render as empty code, which compiles to
	// nothing sensible.
	required := func(path []string, text string) {
		if strings.TrimSpace(text) == "" {
			fail(path, "is required")
			return
		}

		parse(path, text)
	}

	ident := func(path []string, s string) {
		if !token.IsIdentifier(s) {
			fail(path, "%q is no Go identifier", s)
		}
	}

	oneOf := func(path []string, s string, words ...string) {
		if !slices.Contains(words, s) {
			fail(path, "%q; want one of %s", s, strings.Join(words, ", "))
		}
	}

	for _, n := range slices.Sorted(maps.Keys(f.Imports)) {
		ident([]string{"imports", n}, n)

		if isStd(f.Imports[n]) {
			fail([]string{"imports", n},
				"%s is a standard library package; write {{std %q}} in place of it",
				f.Imports[n], f.Imports[n])
		}
	}

	for _, n := range slices.Sorted(maps.Keys(f.Helpers)) {
		ident([]string{"helpers", n}, n)
		required([]string{"helpers", n, "decl"}, f.Helpers[n].Decl)
	}

	for _, key := range slices.Sorted(maps.Keys(f.Types)) {
		at := []string{"types", key}
		t := f.Types[key]

		if _, _, ok := splitTypeKey(key); !ok {
			fail(at, "%q; want <import path>.<type name>", key)
		}

		if t.Holder != nil {
			h := append(slices.Clip(at), "holder")
			ident(append(h, "role"), t.Holder.Role)
			required(append(h, "type"), t.Holder.Type)
			required(append(h, "guard"), t.Holder.Guard)
			required(append(h, "value"), t.Holder.Value)
		}

		if t.Container != nil {
			c := append(slices.Clip(at), "container")
			ident(append(c, "role"), t.Container.Role)
			required(append(c, "type"), t.Container.Type)
			required(append(c, "over"), t.Container.Over)
			oneOf(append(c, "yields"), t.Container.Yields, "values", "pairs")

			if t.Container.Yields == "pairs" {
				oneOf(append(c, "segment"), t.Container.Segment, "index", "key")
			} else if t.Container.Segment != "" {
				oneOf(append(c, "segment"), t.Container.Segment, "index")
			}
		}

		if t.Composite != nil {
			if len(t.Composite.Parts) == 0 {
				fail(append(slices.Clip(at), "composite", "parts"), "is required")
			}

			for i, p := range t.Composite.Parts {
				pp := append(slices.Clip(at), "composite", "parts", strconv.Itoa(i))
				ident(append(pp, "role"), p.Role)
				required(append(pp, "type"), p.Type)
				parse(append(pp, "guard"), p.Guard)
				required(append(pp, "value"), p.Value)

				if slices.ContainsFunc(t.Composite.Parts[:i], func(q TextPart) bool {
					return q.Role == p.Role
				}) {
					fail(append(pp, "role"), "role %s is taken by an earlier part", p.Role)
				}

				switch p.Segment {
				case "", "none":
					parse(append(pp, "label"), p.Label)
				default:
					oneOf(append(pp, "segment"), p.Segment, "none", "index", "key", "field")
					required(append(pp, "label"), p.Label)
				}
			}
		}
	}

	return errs
}

func textError(name string, path []string, format string, args ...any) error {
	return fmt.Errorf(
		"%w: %s: %s",
		ErrTextFile,
		textWhere(name, path),
		fmt.Sprintf(format, args...),
	)
}

// textWarning reports a problem of a file that does not stop the run, as
// a key that names nothing.
func textWarning(name string, path []string, format string, args ...any) error {
	return fmt.Errorf("genext: %s: %s", textWhere(name, path), fmt.Sprintf(format, args...))
}

func textWhere(name string, path []string) string {
	if len(path) == 0 {
		return name
	}

	return name + ": " + strings.Join(
		opiter.SlicesValues(path).
			Map(quoteKey).
			CollectSlice(),
		".",
	)
}

// quoteKey quotes a key that holds more than letters, digits and
// underscores, as a type key holds dots and slashes, so that a key path
// reads back unambiguously.
func quoteKey(k string) string {
	if k != "" &&
		strings.Trim(k, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") == "" {
		return k
	}

	return strconv.Quote(k)
}

// parseText parses text with the functions templates of the file may call,
// bound to nothing.
func parseText(text string) (*template.Template, error) {
	return template.New("").
		Option("missingkey=error").
		Funcs(template.FuncMap{
			"std":    func(string) (string, error) { return "", nil },
			"pkg":    func(string) (string, error) { return "", nil },
			"helper": func(string) (string, error) { return "", nil },
		}).
		Parse(text)
}

func splitTypeKey(key string) (pkgPath, name string, ok bool) {
	i := strings.LastIndex(key, ".")
	if i <= 0 || strings.Contains(key[i+1:], "/") || !token.IsIdentifier(key[i+1:]) {
		return "", "", false
	}

	return key[:i], key[i+1:], true
}
