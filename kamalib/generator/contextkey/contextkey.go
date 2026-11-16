// Package contextkey generates context keys and typed accessor functions.
//
// Each Entry yields one file holding a key variable and the functions
// WithName, ValueName, ValueNameFallback, ValueNameFallbackFn and
// ValueNameDefault for a value of the entry's type. Every generated
// declaration carries the genmark marker naming GeneratorName in its doc
// comment.
//
// A file is named by inserting .contextkey into Entry.FileName before its
// build constraint suffixes, as in sig.contextkey_linux.go for sig_linux.
// It starts with the header base.Header prints: the generated-code line,
// the params package and key_type_func, and the time of the run.
//
// Proc keeps a file that would change in the generated: line only, and
// warns, wrapping base.ErrStale, about each file of its
// output directory that carries the marker of the generator and that the
// run did not produce. Every generated file of a directory must therefore
// come from one step.
//
// Lifted from github.com/ngicks/go-common/contextkey/generate.
package contextkey

import (
	"bytes"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/ngicks/kama/kamalib/generator/base"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/genmark"
)

// GeneratorName is the name the generator marks its declarations with.
const GeneratorName = "kama-contextkey"

func init() { base.MustRegister(GeneratorName) }

var ErrInvalidEntry = errors.New("contextkey: invalid entry")

// generatorPath is the import path of the package, as the header of the
// generated files spells it.
const generatorPath = "github.com/ngicks/kama/kamalib/generator/contextkey"

// naming names the files the generator writes.
var naming = base.Naming{Name: "contextkey"}

// FileName returns the name of the file generated for an Entry whose
// FileName is name.
func FileName(name string) string { return naming.SourceFile(name + ".go") }

// Template is the decoded template file. The json tags of Template and
// Entry give its keys. The keys are lowercase snake case.
type Template struct {
	Entries []Entry `json:"entries"`
}

// Entry describes one context key.
type Entry struct {
	// FileName names the generated file, as FileName tells.
	FileName string `json:"file_name"`
	// Name is used in the key variable and function names.
	Name string `json:"name"`
	// Type is the type of the value associated with the key.
	Type string `json:"type"`
	// DefaultExpr is evaluated by ValueNameDefault when no value is
	// associated.
	DefaultExpr string `json:"default_expr"`
	// Imports are package paths the generated file needs besides "context".
	Imports []string `json:"imports"`
}

func (e Entry) validate() error {
	if e.FileName == "" || strings.ContainsAny(e.FileName, `/\`) {
		return fmt.Errorf("%w: file name %q", ErrInvalidEntry, e.FileName)
	}
	if !token.IsIdentifier(e.Name) {
		return fmt.Errorf("%w: name %q", ErrInvalidEntry, e.Name)
	}
	if _, err := parser.ParseExpr(e.Type); err != nil {
		return fmt.Errorf("%w: type %q: %w", ErrInvalidEntry, e.Type, err)
	}
	if _, err := parser.ParseExpr(e.DefaultExpr); err != nil {
		return fmt.Errorf("%w: default expr %q: %w", ErrInvalidEntry, e.DefaultExpr, err)
	}
	return nil
}

// Options configures Render.
type Options struct {
	// Package is the package clause of generated files.
	Package string
	// KeyTypeFunc names a function in Package that takes a string and
	// returns a key. It must return a distinct key on every call, since
	// entries of the same Type pass it the same string.
	KeyTypeFunc string
	// Now returns the time the header of generated files states. Nil
	// means time.Now.
	Now func() time.Time
	// Registry tells the generators markers name, for the stale file
	// warnings of the proc. Nil means genmark.DefaultRegistry.
	Registry *genmark.Registry
}

func (o Options) withDefaults() Options {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Registry == nil {
		o.Registry = genmark.DefaultRegistry
	}
	return o
}

func (o Options) validate() error {
	if !token.IsIdentifier(o.Package) {
		return fmt.Errorf("contextkey: package %q is not an identifier", o.Package)
	}
	if !token.IsIdentifier(o.KeyTypeFunc) {
		return fmt.Errorf("contextkey: key type func %q is not an identifier", o.KeyTypeFunc)
	}
	return nil
}

// Render returns the source of every entry keyed by file name. The source
// parses but is left unformatted; overlayfs.Fs formats it on write.
func Render(entries []Entry, opts Options) (map[string][]byte, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	opts = opts.withDefaults()
	hdr, err := base.Header(generatorPath, map[string]any{
		"package":       opts.Package,
		"key_type_func": opts.KeyTypeFunc,
	}, opts.Now())
	if err != nil {
		return nil, err
	}
	header := strings.Join(hdr, "\n")

	out := make(map[string][]byte, len(entries))
	for _, e := range entries {
		if err := e.validate(); err != nil {
			return nil, err
		}

		name := FileName(e.FileName)
		if _, dup := out[name]; dup {
			return nil, fmt.Errorf("%w: duplicate file name %q", ErrInvalidEntry, e.FileName)
		}

		src, err := renderEntry(e, opts, header)
		if err != nil {
			return nil, fmt.Errorf("contextkey: render %q: %w", e.FileName, err)
		}
		out[name] = src
	}

	return out, nil
}

func renderEntry(e Entry, opts Options, header string) ([]byte, error) {
	imports := slices.Concat([]string{"context"}, e.Imports)
	slices.Sort(imports)
	imports = slices.Collect(opiter.Map(strconv.Quote, slices.Values(slices.Compact(imports))))

	var buf bytes.Buffer
	err := fileTemplate.Execute(&buf, templateData{
		Entry:       e,
		Header:      header,
		Package:     opts.Package,
		KeyTypeFunc: opts.KeyTypeFunc,
		Imports:     imports,
		Marker:      genmark.Marker(GeneratorName),
	})
	if err != nil {
		return nil, err
	}

	if _, err := parser.ParseFile(
		token.NewFileSet(),
		"",
		buf.Bytes(),
		parser.SkipObjectResolution,
	); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

type templateData struct {
	Entry
	// Header is the comment the file starts with.
	Header      string
	Package     string
	KeyTypeFunc string
	// Imports are quoted import paths.
	Imports []string
	Marker  string
}

var fileTemplate = template.Must(template.New("contextkey").Parse(
	`{{.Header}}

package {{.Package}}

import (
{{- range .Imports}}
	{{.}}
{{- end}}
)

// Key{{.Name}} is the context key for a {{.Type}} value.
//
{{.Marker}}
var Key{{.Name}} = {{.KeyTypeFunc}}({{printf "%q" .Type}})

// With{{.Name}} returns a copy of ctx in which Key{{.Name}} is associated with v.
//
{{.Marker}}
func With{{.Name}}(ctx context.Context, v {{.Type}}) context.Context {
	return context.WithValue(ctx, Key{{.Name}}, v)
}

// Value{{.Name}} returns the {{.Type}} associated with Key{{.Name}} in ctx.
// ok is false if no value is associated or the value is not a {{.Type}}.
//
{{.Marker}}
func Value{{.Name}}(ctx context.Context) (v {{.Type}}, ok bool) {
	v, ok = ctx.Value(Key{{.Name}}).({{.Type}})
	return v, ok
}

// Value{{.Name}}Fallback returns the {{.Type}} associated with Key{{.Name}} in ctx,
// or fallback if Value{{.Name}} reports none.
//
{{.Marker}}
func Value{{.Name}}Fallback(ctx context.Context, fallback {{.Type}}) {{.Type}} {
	if v, ok := Value{{.Name}}(ctx); ok {
		return v
	}
	return fallback
}

// Value{{.Name}}FallbackFn returns the {{.Type}} associated with Key{{.Name}} in ctx,
// or the result of fallbackFn if Value{{.Name}} reports none.
//
{{.Marker}}
func Value{{.Name}}FallbackFn(ctx context.Context, fallbackFn func() {{.Type}}) {{.Type}} {
	if v, ok := Value{{.Name}}(ctx); ok {
		return v
	}
	return fallbackFn()
}

// Value{{.Name}}Default returns the {{.Type}} associated with Key{{.Name}} in ctx,
// or {{.DefaultExpr}} if Value{{.Name}} reports none.
//
{{.Marker}}
func Value{{.Name}}Default(ctx context.Context) {{.Type}} {
	return Value{{.Name}}FallbackFn(ctx, func() {{.Type}} { return {{.DefaultExpr}} })
}
`))
