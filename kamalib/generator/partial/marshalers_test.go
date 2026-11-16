package partial

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"reflect"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"testing"

	"github.com/ngicks/opiter"

	"github.com/ngicks/option/elastic"
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
	"github.com/pelletier/go-toml/v2"
	"go.yaml.in/yaml/v4"
	"gotest.tools/v3/golden"
)

// TestMarshalers records how each encoder treats und.Und, elastic.Elastic and
// opt.Option fields under each omit tag option. The partial generator picks
// the struct tags it emits from these fixtures, so a dependency bump that
// changes the behavior shows up as a fixture diff.
func TestMarshalers(t *testing.T) {
	for _, f := range probeFormats {
		t.Run(f.name, func(t *testing.T) {
			var b strings.Builder
			fmt.Fprintf(&b, "# Produced by %s (%s; %s) using %s.\n",
				t.Name(), goMinorVersion(), moduleVersions(t, f.modules), f.api)
			fmt.Fprintf(
				&b,
				"# Regenerate with: go test ./generator/partial -run %s -update\n",
				t.Name(),
			)
			for _, option := range f.options {
				b.WriteString("\n")
				probe(&b, f, option)
			}
			golden.Assert(t, b.String(), "marshalers/"+f.name+".txt")
		})
	}
}

type probeFormat struct {
	name    string
	tagKey  string
	api     string
	modules []string
	options []string
	marshal func(v any) ([]byte, error)
	decode  func(data []byte, v any) error
	// doc returns a document holding key in the given state.
	// ok is false when the format cannot express the state.
	doc func(key string, state docState) (doc string, ok bool)
}

type docState string

const (
	docMissing docState = "missing"
	docNull    docState = "null"
	docValue   docState = "value"
)

const optionModule = "github.com/ngicks/option"

var probeFormats = []probeFormat{
	{
		name:    "json_v1",
		tagKey:  "json",
		api:     "encoding/json Marshal/Unmarshal",
		modules: []string{optionModule},
		options: []string{"", "omitempty", "omitzero"},
		marshal: json.Marshal,
		decode:  json.Unmarshal,
		doc:     jsonDoc,
	},
	{
		name:    "json_v2",
		tagKey:  "json",
		api:     "encoding/json/v2 Marshal/Unmarshal",
		modules: []string{optionModule},
		options: []string{"", "omitempty", "omitzero"},
		marshal: func(v any) ([]byte, error) { return jsonv2.Marshal(v) },
		decode:  func(data []byte, v any) error { return jsonv2.Unmarshal(data, v) },
		doc:     jsonDoc,
	},
	{
		name:    "yaml_v4",
		tagKey:  "yaml",
		api:     "yaml.Marshal/yaml.Unmarshal",
		modules: []string{optionModule, "go.yaml.in/yaml/v4"},
		options: []string{"", "omitempty", "omitzero"},
		marshal: yaml.Marshal,
		decode:  yaml.Unmarshal,
		doc: func(key string, state docState) (string, bool) {
			switch state {
			case docNull:
				return key + ": null\n", true
			case docValue:
				return key + ": x\n", true
			}
			return "{}\n", true
		},
	},
	{
		name:    "toml_v2",
		tagKey:  "toml",
		api:     "toml.Marshal/toml.Unmarshal",
		modules: []string{optionModule, "github.com/pelletier/go-toml/v2"},
		options: []string{"", "omitempty", "omitzero"},
		marshal: toml.Marshal,
		decode:  toml.Unmarshal,
		doc: func(key string, state docState) (string, bool) {
			switch state {
			case docNull:
				return "", false
			case docValue:
				return key + ` = "x"` + "\n", true
			}
			return "", true
		},
	},
}

func jsonDoc(key string, state docState) (string, bool) {
	switch state {
	case docNull:
		return `{"` + key + `":null}`, true
	case docValue:
		return `{"` + key + `":"x"}`, true
	}
	return `{}`, true
}

type probeField struct {
	key   string
	value any
}

var (
	encodeFields = []probeField{
		{"und_undefined", und.Undefined[string]()},
		{"und_null", und.Null[string]()},
		{"und_defined", und.Defined("x")},
		{"elastic_undefined", elastic.Undefined[string]()},
		{"elastic_null", elastic.Null[string]()},
		{"elastic_defined", elastic.FromValue("x")},
		{"opt_none", opt.None[string]()},
		{"opt_some", opt.Some("x")},
	}
	decodeFields = []probeField{
		{"und", und.Und[string]{}},
		{"elastic", elastic.Elastic[string]{}},
		{"opt", opt.Option[string]{}},
	}
)

// probeStruct builds a struct type at run time so that one field list serves
// every tag key and option combination.
func probeStruct(fields []probeField, tagKey, option string) reflect.Type {
	toStructField := func(i int, f probeField) reflect.StructField {
		tag := f.key
		if option != "" {
			tag += "," + option
		}
		return reflect.StructField{
			Name: fmt.Sprintf("F%d", i),
			Type: reflect.TypeOf(f.value),
			Tag:  reflect.StructTag(fmt.Sprintf("%s:%q", tagKey, tag)),
		}
	}
	return reflect.StructOf(slices.Collect(
		opiter.WrapSeq2(slices.All(fields)).
			Unify(toStructField).
			Iter(),
	))
}

func probe(b *strings.Builder, f probeFormat, option string) {
	tag := fmt.Sprintf("%s:%q", f.tagKey, "<key>")
	if option != "" {
		tag = fmt.Sprintf("%s:%q", f.tagKey, "<key>,"+option)
	}
	fmt.Fprintf(b, "## tag `%s`\n", tag)

	encType := probeStruct(encodeFields, f.tagKey, option)
	enc := reflect.New(encType).Elem()
	for i, field := range encodeFields {
		enc.Field(i).Set(reflect.ValueOf(field.value))
	}
	out, err := recovered(func() ([]byte, error) { return f.marshal(enc.Addr().Interface()) })
	b.WriteString("encode:\n")
	writeIndented(b, string(out))
	if err != nil {
		fmt.Fprintf(b, "encode error: %s\n", oneLine(err, encType))
	}

	b.WriteString("decode:\n")
	decType := probeStruct(decodeFields, f.tagKey, option)
	for i, field := range decodeFields {
		for _, state := range []docState{docMissing, docNull, docValue} {
			doc, ok := f.doc(field.key, state)
			if !ok {
				fmt.Fprintf(b, "  %s %s: not expressible\n", field.key, state)
				continue
			}
			dec := reflect.New(decType)
			_, err := recovered(
				func() ([]byte, error) { return nil, f.decode([]byte(doc), dec.Interface()) },
			)
			fmt.Fprintf(
				b,
				"  %s %s: %s",
				field.key,
				state,
				describe(dec.Elem().Field(i).Interface()),
			)
			if err != nil {
				fmt.Fprintf(b, " (error: %s)", oneLine(err, decType))
			}
			b.WriteString("\n")
		}
	}
}

// recovered turns a panic into an error because some encoders report invalid
// struct tags by panicking and the fixture should record that outcome.
func recovered(fn func() ([]byte, error)) (out []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return fn()
}

func describe(v any) string {
	switch x := v.(type) {
	case und.Und[string]:
		switch {
		case x.IsUndefined():
			return "undefined"
		case x.IsNull():
			return "null"
		}
		return fmt.Sprintf("defined(%q)", x.Value())
	case elastic.Elastic[string]:
		switch {
		case x.IsUndefined():
			return "undefined"
		case x.IsNull():
			return "null"
		}
		vals := slices.Collect(
			opiter.WrapSeq(slices.Values(x.Options())).
				Map(describeOption).
				Iter(),
		)
		return "defined[" + strings.Join(vals, ", ") + "]"
	case opt.Option[string]:
		if x.IsNone() {
			return "none"
		}
		return fmt.Sprintf("some(%q)", x.Value())
	}
	panic(fmt.Sprintf("unexpected probe field type %T", v))
}

func describeOption(o opt.Option[string]) string { return describe(o) }

func writeIndented(b *strings.Builder, s string) {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		b.WriteString("  (empty)\n")
		return
	}
	for line := range strings.SplitSeq(s, "\n") {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
}

// oneLine shortens the run-time built probe type in error messages because
// its full literal repeats every field and tag.
func oneLine(err error, probeType reflect.Type) string {
	s := strings.ReplaceAll(err.Error(), probeType.String(), "<probe struct>")
	return strings.ReplaceAll(s, "\n", `\n`)
}

// goMinorVersion drops the patch version so that toolchain patch releases do
// not invalidate the fixtures.
func goMinorVersion() string {
	v := runtime.Version()
	if i := strings.Index(v, "."); i >= 0 {
		if j := strings.Index(v[i+1:], "."); j >= 0 {
			return v[:i+1+j]
		}
	}
	return v
}

func moduleVersions(t *testing.T, paths []string) string {
	t.Helper()
	info, ok := debug.ReadBuildInfo()
	if !ok {
		t.Fatal("build info is unavailable")
	}
	versions := make([]string, 0, len(paths))
	for _, p := range paths {
		isModule := func(dep *debug.Module) bool { return dep.Path == p }
		dep := opiter.WrapSeq(slices.Values(info.Deps)).Filter(isModule).First()
		if dep.IsNone() {
			t.Fatalf("module %s is missing from build info", p)
		}
		versions = append(versions, p+" "+dep.Value().Version)
	}
	return strings.Join(versions, ", ")
}
