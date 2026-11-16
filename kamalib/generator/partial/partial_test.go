package partial

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ngicks/opiter"

	"golang.org/x/tools/go/packages"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/genmark"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
)

// load type-checks files, keyed by slash-separated path relative to a
// directory of its own under testdata, as packages of this module. The files
// live in the overlay only.
func load(t *testing.T, files map[string]string) []*packages.Package {
	t.Helper()

	ctx := context.Background()
	f, err := overlayfs.New(".")
	assert.NilError(t, err)
	t.Cleanup(func() { f.Close() })

	dir := "testdata/inline/" + strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	dirs := map[string]bool{}
	for name, src := range files {
		assert.NilError(t, f.Write(ctx, path.Join(dir, name), []byte(src)))
		dirs["./"+path.Dir(path.Join(dir, name))] = true
	}

	pkgs, err := gopackages.Load(ctx, f, gopackages.Options{
		Patterns: slices.Sorted(maps.Keys(dirs)),
	})
	assert.NilError(t, err)
	return pkgs
}

// registry returns a registry of this generator only.
func registry(t *testing.T) *genmark.Registry {
	t.Helper()
	r := genmark.NewRegistry()
	assert.NilError(t, r.Register(GeneratorName))
	return r
}

// testNow is the time the header of the files generate gives records.
func testNow() time.Time { return time.Date(2026, 11, 4, 10, 12, 33, 0, time.UTC) }

// header is the first line of every generated file.
var header = genhelper.GeneratedComment(generatorPath)

// testHeader is the header of the files generate gives with default options.
var testHeader = header + `
// params:
//	merge: "overwrite"
//	merge_nested: "recursive"
//	option_file: null
// generated: 2026-11-04T10:12:33Z`

var (
	optionFileParam = regexp.MustCompile(`(?m)^(//\toption_file: ).*$`)
	generatedLine   = regexp.MustCompile(`(?m)^` + genhelper.GeneratedTimePrefix + `.*\n`)
)

// withoutRunParams returns src, a generated file, without the generated:
// line and with a null option_file, the header fields that tell how a run
// was made rather than what it generated.
func withoutRunParams(src string) string {
	if loc := generatedLine.FindStringIndex(src); loc != nil {
		src = src[:loc[0]] + src[loc[1]:]
	}
	return optionFileParam.ReplaceAllString(src, "${1}null")
}

// generate runs Generate on files with opts, a registry of this generator
// only and testNow, and fails on a warning.
func generate(t *testing.T, files map[string]string, opts Options) ([]File, error) {
	t.Helper()
	if opts.Registry == nil {
		opts.Registry = registry(t)
	}
	if opts.Now == nil {
		opts.Now = testNow
	}
	out, warnings, err := Generate(load(t, files), opts)
	assert.Assert(t, len(warnings) == 0, "%v", warnings)
	return out, err
}

// generatedWith returns the files generated with opts keyed by base name.
func generatedWith(t *testing.T, files map[string]string, opts Options) map[string]string {
	t.Helper()

	out, err := generate(t, files, opts)
	assert.NilError(t, err)
	m := map[string]string{}
	for _, f := range out {
		m[filepath.Base(f.Path)] = string(f.Content)
	}
	return m
}

// generated returns the generated files keyed by base name.
func generated(t *testing.T, files map[string]string) map[string]string {
	t.Helper()
	return generatedWith(t, files, Options{})
}

// partialTypes returns the type names declared in src.
func partialTypes(t *testing.T, src string) []string {
	t.Helper()

	f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
	assert.NilError(t, err)
	specs := opiter.Flatten(opiter.WrapSeq(slices.Values(f.Decls)).Map(typeSpecs))
	return opiter.WrapSeq(specs).Map(typeSpecName).Collect()
}

// typeSpecs returns the specs of d when it declares types.
func typeSpecs(d ast.Decl) []ast.Spec {
	if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
		return gd.Specs
	}
	return nil
}

func typeSpecName(s ast.Spec) string { return s.(*ast.TypeSpec).Name.Name }

func TestOptions(t *testing.T) {
	assert.NilError(t, Options{}.Validate())
	assert.NilError(t, Options{Merge: MergeAppend, MergeNested: MergeNestedOverwrite}.Validate())
	assert.ErrorIs(t, Options{Merge: "x"}.Validate(), ErrOption)
	assert.ErrorIs(t, Options{MergeNested: "x"}.Validate(), ErrOption)
	def := Options{}.WithDefaults()
	assert.Equal(t, def.Merge, MergeOverwrite)
	assert.Equal(t, def.MergeNested, MergeNestedRecursive)
	assert.Equal(t, def.Registry, genmark.DefaultRegistry)
	r := genmark.NewRegistry()
	assert.Equal(t, Options{Registry: r}.WithDefaults().Registry, r)

	i := &instance{}
	assert.ErrorContains(t, i.Param(ProcParam{Merge: "replace"}), "merge")
	assert.ErrorContains(t, i.Param(ProcParam{MergeNested: "deep"}), "merge_nested")
	assert.NilError(t, i.Param(ProcParam{Merge: "append"}))
	assert.Equal(t, i.opts.Merge, MergeAppend)
	assert.Equal(t, i.opts.MergeNested, MergeNestedRecursive)
	assert.Equal(t, i.opts.Registry, genmark.DefaultRegistry)
}

func TestTargetErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		is   error
		// contains are substrings the error must hold.
		contains []string
	}{
		{
			name:     "unknown type args",
			src:      "package x\n\n//kama:partial file\ntype A struct{}\n",
			is:       ErrDirective,
			contains: []string{"x.go:3:1", `"file"`, "_unknown_type_args.A"},
		},
		{
			name:     "bare free-floating",
			src:      "package x\n\n//kama:partial\n\ntype A struct{}\n",
			is:       ErrDirective,
			contains: []string{"x.go:3:1"},
		},
		{
			name:     "ignore free-floating",
			src:      "package x\n\n//kama:partial ignore\n\ntype A struct{}\n",
			is:       ErrDirective,
			contains: []string{"x.go:3:1", `"ignore"`},
		},
		{
			name:     "field",
			src:      "package x\n\ntype A struct {\n\t//kama:partial\n\tB int\n}\n",
			is:       ErrDirective,
			contains: []string{"x.go:4:2", "_field.A.B"},
		},
		{
			name:     "marked and ignored",
			src:      "package x\n\n//kama:partial\n//kama:partial ignore\ntype A struct{}\n",
			is:       ErrDirective,
			contains: []string{"x.go:3:1", "both"},
		},
		{
			name:     "basic",
			src:      "package x\n\n//kama:partial\ntype A int\n",
			is:       ErrUnsupported,
			contains: []string{"x.go:3:1", "_basic.A", "int"},
		},
		{
			name:     "interface",
			src:      "package x\n\n//kama:partial\ntype A interface{ M() }\n",
			is:       ErrUnsupported,
			contains: []string{"x.go:3:1", "_interface.A"},
		},
		{
			name:     "func",
			src:      "package x\n\n//kama:partial\ntype A func()\n",
			is:       ErrUnsupported,
			contains: []string{"x.go:3:1"},
		},
		{
			name:     "alias",
			src:      "package x\n\ntype B struct{}\n\n//kama:partial\ntype A = B\n",
			is:       ErrUnsupported,
			contains: []string{"x.go:5:1", "alias"},
		},
		{
			name: "pointer to pointer",
			src:  "package x\n\n//kama:partial\ntype A struct {\n\tB **int\n}\n",
			is:   ErrUnsupported,
			contains: []string{
				"testdata/inline/TestTargetErrors_pointer_to_pointer.A",
				"field B",
				"x.go:5:2",
				"**int",
			},
		},
		{
			name: "pointer to pointer element of a slice target",
			src:  "package x\n\n//kama:partial\ntype A []**B\n\ntype B struct{ I int }\n",
			is:   ErrUnsupported,
			contains: []string{
				"_pointer_to_pointer_element_of_a_slice_target.A",
				"x.go:4:6",
				"pointer to pointer",
			},
		},
		{
			name:     "pointer to pointer element of a map target",
			src:      "package x\n\n//kama:partial\ntype A map[string]**B\n\ntype B struct{ I int }\n",
			is:       ErrUnsupported,
			contains: []string{"x.go:4:6", "pointer to pointer"},
		},
		{
			name: "pointer to pointer to a slice target",
			src: "package x\n\n//kama:partial\ntype A struct {\n\tB **B\n}\n\n" +
				"type B []C\n\ntype C struct{ I int }\n",
			is:       ErrUnsupported,
			contains: []string{"field B", "x.go:5:2", "pointer to pointer"},
		},
		{
			name:     "array of targets",
			src:      "package x\n\n//kama:partial\ntype A [2]B\n\ntype B struct{ I int }\n",
			is:       ErrUnsupported,
			contains: []string{"x.go:3:1", "want a struct, slice or map"},
		},
		{
			name:     "embedded recursive",
			src:      "package x\n\n//kama:partial\ntype A struct {\n\t*A\n}\n",
			is:       ErrUnsupported,
			contains: []string{"A -> A", "embedded", "x.go:4:6"},
		},
		{
			name: "embedded recursive through a value",
			src: "package x\n\n//kama:partial\ntype A struct {\n\tB B\n}\n\n" +
				"type B struct {\n\t*A\n}\n",
			is:       ErrUnsupported,
			contains: []string{"A -> B -> A"},
		},
		{
			name:     "unknown sub-directive",
			src:      "package x\n\n//kama:partial\n//kama:partial:name B\ntype A struct{}\n",
			is:       ErrDirective,
			contains: []string{"x.go:4:1", "partial:name"},
		},
		{
			name:     "merge on a type",
			src:      "package x\n\n//kama:partial\n//kama:partial:merge append\ntype A struct{}\n",
			is:       ErrDirective,
			contains: []string{"x.go:4:1", "struct fields only"},
		},
		{
			name: "unknown merge strategy",
			src: "package x\n\n//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:merge deep\n\tB []int\n}\n",
			is:       ErrDirective,
			contains: []string{"x.go:5:2", `"deep"`, "_unknown_merge_strategy.A.B"},
		},
		{
			name: "second merge",
			src: "package x\n\n//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:merge append\n\tB []int //kama:partial:merge overwrite\n}\n",
			is:       ErrDirective,
			contains: []string{"x.go:6:10", "x.go:5:2", "second"},
		},
		{
			name: "append on a plain field",
			src: "package x\n\n//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:merge append\n\tB int\n}\n",
			is:       ErrDirective,
			contains: []string{"x.go:5:2", "_append_on_a_plain_field.A.B", "want overwrite"},
		},
		{
			name: "append on a nested field",
			src: "package x\n\n//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:merge append\n\tB B\n}\n\ntype B struct{ I int }\n",
			is:       ErrDirective,
			contains: []string{"x.go:5:2", "A.B", "want recursive or overwrite"},
		},
		{
			name: "recursive on a slice field",
			src: "package x\n\n//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:merge recursive\n\tB []B\n}\n\ntype B struct{ I int }\n",
			is:       ErrDirective,
			contains: []string{"x.go:5:2", "want overwrite or append"},
		},
		{
			name: "append on an elastic field",
			src: "package x\n\nimport \"github.com/ngicks/option/elastic\"\n\n" +
				"//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:merge append\n\tB elastic.Elastic[int]\n}\n",
			is:       ErrDirective,
			contains: []string{"x.go:7:2", "want overwrite"},
		},
		{
			name: "merge on a type without partial variant",
			src: "package x\n\n//kama:partial\ntype A struct{}\n\n" +
				"type B struct {\n\t//kama:partial:merge append\n\tC []int\n}\n",
			is:       ErrDirective,
			contains: []string{"x.go:7:2", "B has no partial variant"},
		},
		{
			name: "type parameter shadows a partial",
			src: "package x\n\n//kama:partial\ntype A[BPartial any] struct{ X B }\n\n" +
				"type B struct{ I int }\n",
			is:       ErrConflict,
			contains: []string{"x.go:4:8", "type parameter BPartial", "_partial.A"},
		},
		{
			name:     "type parameter shadows its own partial",
			src:      "package x\n\n//kama:partial\ntype A[APartial any] struct{ X int }\n",
			is:       ErrConflict,
			contains: []string{"type parameter APartial"},
		},
		{
			name: "duplicate partial field",
			src: "package x\n\n//kama:partial\ntype A struct {\n\tM\n\tMPartial int\n}\n\n" +
				"type M struct{ I int }\n",
			is:       ErrConflict,
			contains: []string{"field MPartial (", "x.go:6:2", "given by M too", "x.go:5:2"},
		},
		{
			name: "duplicate embedded through an alias",
			src: "package x\n\n//kama:partial\ntype A struct {\n\tM\n\tAlias\n}\n\n" +
				"type M struct{ I int }\n\ntype Alias = M\n",
			is:       ErrConflict,
			contains: []string{"field Alias", "MPartial"},
		},
		{
			name:     "field named like a method",
			src:      "package x\n\n//kama:partial\ntype A struct {\n\tMerge int\n}\n",
			is:       ErrConflict,
			contains: []string{"x.go:5:2", "method Merge"},
		},
		{
			name:     "name conflict",
			src:      "package x\n\n//kama:partial\ntype A struct{}\n\ntype APartial struct{}\n",
			is:       ErrConflict,
			contains: []string{"x.go:6:6", "APartial"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := generate(t, map[string]string{"x.go": c.src}, Options{})
			assert.ErrorIs(t, err, c.is)
			for _, s := range c.contains {
				assert.ErrorContains(t, err, s)
			}
		})
	}
}

func TestTargetSelection(t *testing.T) {
	files := generated(t, map[string]string{
		"a.go": `package x

//kama:partial
type A struct {
	B     B
	C     *C
	D     []D
	E     map[string]E
	F     [1]F
	K     map[K]int
	Ch    chan Ch
	Arg   Box[Arg]
	Ign   Ign
	Basic Basic
}

//kama:partial ignore
type Ign struct{ Next N }

type B struct{}
type C struct{}
type D struct{}
type E struct{}
type F struct{}
type K struct{}
type Ch struct{}
type Arg struct{}
type Box[T any] struct{ V T }
type Basic string
type N struct{}
type Unrelated struct{}
`,
		"file.go": `package x

//kama:partial file

type Marked struct{}

//kama:partial ignore
type Skipped struct{}

type Func func()
type Iface interface{}
type Int int
type Alias = Marked
`,
		"sub/pkg.go": `// Package sub is marked as a whole.
//
//kama:partial package
package sub
`,
		"sub/types.go": `package sub

type S struct{}

type L []S
`,
	})

	assert.DeepEqual(t, slices.Sorted(maps.Keys(files)), []string{
		"a.kama_partial.go",
		"file.kama_partial.go",
		"types.kama_partial.go",
		FlagSetFile,
		HelpersFile,
	})
	assert.DeepEqual(t, partialTypes(t, files["a.kama_partial.go"]), []string{
		"APartial", "BPartial", "CPartial", "DPartial", "EPartial", "FPartial", "BoxPartial",
	})
	assert.DeepEqual(t, partialTypes(t, files["file.kama_partial.go"]), []string{"MarkedPartial"})
	assert.DeepEqual(
		t,
		partialTypes(t, files["types.kama_partial.go"]),
		[]string{"SPartial", "LPartial"},
	)
}

func TestGeneratedDeclsAreNotTargets(t *testing.T) {
	files := generated(t, map[string]string{
		"a.go": "// Package x is marked.\n//\n//kama:partial package\npackage x\n\ntype A struct{}\n",
		"a.kama_partial.go": "package x\n\n" +
			"//kama:generated kama-partial\ntype APartial struct{}\n\n" +
			"// Leftover is marked by a registered generator.\n//\n" +
			"//kama:generated kama-partial\ntype Leftover struct{}\n",
		"other.go": "package x\n\n//kama:generated unregistered\ntype O struct{}\n",
	})
	assert.DeepEqual(t, partialTypes(t, files["a.kama_partial.go"]), []string{"APartial"})
	assert.DeepEqual(t, partialTypes(t, files["other.kama_partial.go"]), []string{"OPartial"})
}

// withoutTag drops the tag of an entry of the result of fieldTypes.
func withoutTag(name string, typeAndTag [2]string) (string, string) {
	return name, typeAndTag[0]
}

// fieldTypes returns the type and the tag of each field of the struct type
// name declared in src, keyed by field name; embedded fields are keyed by
// their type.
func fieldTypes(t *testing.T, src, name string) map[string][2]string {
	t.Helper()

	f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
	assert.NilError(t, err)
	var st *ast.StructType
	ast.Inspect(f, func(n ast.Node) bool {
		if ts, ok := n.(*ast.TypeSpec); ok && ts.Name.Name == name {
			st, _ = ts.Type.(*ast.StructType)
		}
		return st == nil
	})
	assert.Assert(t, st != nil, name)

	out := map[string][2]string{}
	for _, fl := range st.Fields.List {
		var tag string
		if fl.Tag != nil {
			tag = fl.Tag.Value
		}
		ty := types.ExprString(fl.Type)
		if len(fl.Names) == 0 {
			out["embedded "+ty] = [2]string{ty, tag}
		}
		for _, n := range fl.Names {
			out[n.Name] = [2]string{ty, tag}
		}
	}
	return out
}

func TestMapping(t *testing.T) {
	files := generated(t, map[string]string{
		"a.go": `package x

import (
	"time"

	"github.com/ngicks/option/elastic"
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
)

//kama:partial
type A struct {
	M
	*P
	time.Time
	Basic

	Plain   int
	Ptr     *int
	Target  M
	TPtr    *P
	Slice   []M
	Map     map[string]M
	Array   [2]M
	Opt     opt.Option[int]
	OptPtr  *opt.Option[int]
	Und     und.Und[int]
	Elastic elastic.Elastic[int]
	Inst    G[int, M]
	Func    func()
	Chan    chan int
	Iface   interface{ M() }
	Any     any
	Anon    struct{ X int }
	Alias   AliasM
	private int
}

type M struct{ X int }
type P struct{ X int }
type Basic string
type G[K comparable, V any] struct {
	K K
	V V
}
type AliasM = M

//kama:partial
type S []*M

//kama:partial
type Om map[int]opt.Option[M]
`,
	})
	src := files["a.kama_partial.go"]

	want := map[string]string{
		"embedded MPartial": "MPartial",
		"embedded PPartial": "PPartial",
		"Time":              "und.Und[time.Time]",
		"Basic":             "und.Und[Basic]",
		"Plain":             "und.Und[int]",
		"Ptr":               "und.Und[int]",
		"Target":            "und.Und[MPartial]",
		"TPtr":              "und.Und[PPartial]",
		"Slice":             "und.Und[[]M]",
		"Map":               "und.Und[map[string]M]",
		"Array":             "und.Und[[2]M]",
		"Opt":               "und.Und[int]",
		"OptPtr":            "und.Und[opt.Option[int]]",
		"Und":               "und.Und[int]",
		"Elastic":           "elastic.Elastic[int]",
		"Inst":              "und.Und[GPartial[int, M]]",
		"Func":              "und.Und[func()]",
		"Chan":              "und.Und[chan int]",
		"Iface":             "und.Und[interface{M()}]",
		"Any":               "und.Und[any]",
		"Anon":              "und.Und[struct{X int}]",
		"Alias":             "und.Und[MPartial]",
		"private":           "und.Und[int]",
	}
	got := maps.Collect(
		opiter.WrapSeq2(maps.All(fieldTypes(t, src, "APartial"))).
			Map(withoutTag).
			Iter2(),
	)
	assert.DeepEqual(t, got, want)

	assert.Assert(t, strings.Contains(src, "type SPartial []und.Und[MPartial]"), src)
	assert.Assert(t, strings.Contains(src, "type OmPartial map[int]und.Und[M]"), src)
	assert.Assert(t, strings.Contains(src, "type GPartial[K comparable, V any] struct"), src)
}

func TestImports(t *testing.T) {
	files := generated(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct{ B }\n",
		"b.go": "package x\n\ntype B struct{ X int }\n",
	})
	// a.go has an embedded target only, so its file needs no und import.
	assert.Assert(
		t,
		!strings.Contains(files["a.kama_partial.go"], "import"),
		files["a.kama_partial.go"],
	)
	assert.Assert(
		t,
		strings.Contains(files["b.kama_partial.go"], `import "github.com/ngicks/option/und"`),
		files["b.kama_partial.go"],
	)
	for _, src := range files {
		assert.Assert(t, strings.HasPrefix(src, testHeader+"\n\npackage x\n"), src)
		assert.Assert(t, strings.Contains(src, "\n//kama:generated kama-partial\ntype "), src)
	}
}

func TestBuildConstraint(t *testing.T) {
	files := generated(t, map[string]string{
		"a.go":       "//go:build !never\n\npackage x\n\n//kama:partial\ntype A struct{ X int }\n",
		"b_linux.go": "package x\n\n//kama:partial\ntype B struct{ X int }\n",
	})
	assert.Assert(
		t,
		strings.HasPrefix(
			files["a.kama_partial.go"],
			testHeader+"\n\n//go:build !never\n\npackage x\n",
		),
		files["a.kama_partial.go"],
	)
	assert.Assert(
		t,
		strings.HasPrefix(files["b.kama_partial_linux.go"], testHeader+"\n\npackage x\n"),
	)
}

func TestPartialTag(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{``, `json:",omitzero"`},
		{`json:"a"`, `json:"a,omitzero"`},
		{`json:"a,omitempty"`, `json:"a,omitzero"`},
		{`json:"a,omitempty,string"`, `json:"a,string,omitzero"`},
		{`json:"a,omitzero"`, `json:"a,omitzero"`},
		{`json:",omitempty"`, `json:",omitzero"`},
		{`json:"-"`, `json:"-"`},
		{`json:"-,"`, `json:"-,omitzero"`},
		{`yaml:"a"`, `yaml:"a,omitempty" json:",omitzero"`},
		{`yaml:"a,omitzero"`, `yaml:"a,omitempty" json:",omitzero"`},
		{`yaml:"a,omitzero,flow"`, `yaml:"a,flow,omitempty" json:",omitzero"`},
		{`yaml:"a,omitempty,flow"`, `yaml:"a,omitempty,flow" json:",omitzero"`},
		{`json:"a,"`, `json:"a,omitzero"`},
		{`yaml:"-"`, `yaml:"-" json:",omitzero"`},
		{`toml:"a"`, `toml:"a,omitzero" json:",omitzero"`},
		{`toml:"a,omitempty"`, `toml:"a,omitzero" json:",omitzero"`},
		{`toml:"-"`, `toml:"-" json:",omitzero"`},
		{`xml:"a,omitempty" db:"b"`, `xml:"a,omitempty" db:"b" json:",omitzero"`},
		{
			`json:"a,omitempty" yaml:"b,omitzero" toml:"c,omitempty"`,
			`json:"a,omitzero" yaml:"b,omitempty" toml:"c,omitzero"`,
		},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := partialTag(c.in, true)
			assert.NilError(t, err)
			assert.Equal(t, got, c.want)
		})
	}

	for in, want := range map[string]string{
		``:                         ``,
		`yaml:"a"`:                 `yaml:"a,omitempty"`,
		`json:"a,omitempty"`:       `json:"a,omitzero"`,
		`toml:"a,omitempty"`:       `toml:"a,omitzero"`,
		`db:"a" json:",omitempty"`: `db:"a" json:",omitzero"`,
	} {
		got, err := partialTag(in, false)
		assert.NilError(t, err)
		assert.Equal(t, got, want, "unexported %s", in)
	}

	_, err := partialTag(`json:a`, true)
	assert.Assert(t, err != nil)
}

// TestSharedFileNames pins the exported file names to the naming of the
// generator.
func TestSharedFileNames(t *testing.T) {
	assert.Equal(t, HelpersFile, naming.SharedFile(helperKind))
	assert.Equal(t, FlagSetFile, naming.SharedFile("flagset"))
	assert.Equal(t, FileSuffix, naming.Suffix())
}
