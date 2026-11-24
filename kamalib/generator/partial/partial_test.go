package partial

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"iter"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ngicks/opiter"
	"github.com/ngicks/option/und"
	"golang.org/x/tools/go/packages"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/genopt"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/genheader"
	"github.com/ngicks/kama/kamalib/pkg/genmark"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
)

// registry returns a registry of this generator only.
func registry(t *testing.T) *genmark.Registry {
	t.Helper()

	r := genmark.NewRegistry(base.ToolName)
	assert.NilError(t, r.Register(GeneratorId.String()))

	return r
}

// header is the first line of every generated file.
var header = genheader.GeneratedComment(GeneratorId.String())

// testHeader is the header of the files generate gives with default options.
var testHeader = header + `
// params:
//	merge: "overwrite"
//	merge_nested: "recursive"
//	option_file: null
//	package_option_file: null
// generated: 2026-11-04T10:12:33Z`

var (
	optionFileParam = regexp.MustCompile(`(?m)^(//\t(?:package_)?option_file: ).*$`)
	generatedLine   = regexp.MustCompile(`(?m)^` + genheader.GeneratedTimePrefix + `.*\n`)
)

// withoutRunParams returns src, a generated file, without the generated:
// line and with a null option_file and package_option_file, the header
// fields that tell how a run was made rather than what it generated.
func withoutRunParams(src string) string {
	if loc := generatedLine.FindStringIndex(src); loc != nil {
		src = src[:loc[0]] + src[loc[1]:]
	}

	return optionFileParam.ReplaceAllString(src, "${1}null")
}

// testRun configures a run of Generator in a test. Registry is a registry
// of this generator only when nil, Now is gentest.Now when nil, and Loaded
// holds the directives of the packages only when nil.
type testRun struct {
	Options
	Registry *genmark.Registry
	Now      func() time.Time
	Loaded   *LoadedOptions
}

// runGenerate runs Generator with r on pkgs, loaded from f, as a run of the
// proc does without staging. It returns the files, with absolute paths, the
// warnings of the loaded options followed by those of the run, and the
// error.
func runGenerate(
	t *testing.T,
	f *overlayfs.Fs,
	pkgs []*packages.Package,
	r testRun,
) ([]base.File, []error, error) {
	t.Helper()

	if r.Registry == nil {
		r.Registry = registry(t)
	}

	if r.Now == nil {
		r.Now = gentest.Now
	}

	in := Input{Packages: base.SourcePackages(pkgs), Options: r.Options, Root: f.Root()}

	in.Loaded = r.Loaded
	if in.Loaded == nil {
		loaded, err := NewLoader(nil).LoadPackages(in.Packages)
		if err != nil {
			return nil, nil, loadError(err)
		}

		in.Loaded = loaded
	}

	files, warnings, err := gentest.Run(
		t.Context(), Generator, r.Registry, r.Now(), in, in.Scope(), f,
	)

	return files, slices.Concat(in.Loaded.Warnings, warnings), err
}

// generate runs Generator on files with r and fails on a warning.
func generate(t *testing.T, files map[string]string, r testRun) ([]base.File, error) {
	t.Helper()

	f, pkgs := gentest.LoadFs(t, files)
	out, warnings, err := runGenerate(t, f, pkgs, r)
	assert.Assert(t, len(warnings) == 0, "%v", warnings)

	return out, err
}

// generatedWith returns the files generated with r keyed by base name.
func generatedWith(t *testing.T, files map[string]string, r testRun) map[string]string {
	t.Helper()

	out, err := generate(t, files, r)
	assert.NilError(t, err)

	return gentest.ByBase(out)
}

// generated returns the generated files keyed by base name.
func generated(t *testing.T, files map[string]string) map[string]string {
	t.Helper()
	return generatedWith(t, files, testRun{})
}

// partialTypes returns the type names declared in src.
func partialTypes(t *testing.T, src string) []string {
	t.Helper()

	f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
	assert.NilError(t, err)

	return opiter.WrapSeq(slices.Values(f.Decls)).
		FlatMap(func(d ast.Decl) iter.Seq[ast.Spec] { return slices.Values(typeSpecs(d)) }).
		Map(func(s ast.Spec) string { return s.(*ast.TypeSpec).Name.Name }).
		CollectSlice()
}

// typeSpecs returns the specs of d when it declares types.
func typeSpecs(d ast.Decl) []ast.Spec {
	if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
		return gd.Specs
	}

	return nil
}

func TestOptions(t *testing.T) {
	assert.NilError(t, Options{}.Validate())
	assert.NilError(t, Options{Merge: MergeAppend, MergeNested: MergeNestedOverwrite}.Validate())
	assert.ErrorIs(t, Options{Merge: "x"}.Validate(), ErrOption)
	assert.ErrorIs(t, Options{MergeNested: "x"}.Validate(), ErrOption)

	def := Options{}.WithDefaults()
	assert.Equal(t, def.Merge, MergeOverwrite)
	assert.Equal(t, def.MergeNested, MergeNestedRecursive)

	i := newInstance(t, proc)
	assert.ErrorContains(
		t,
		i.Param(ProcParamPartial{MergeStrategy: und.Defined("replace")}),
		"merge",
	)
	assert.ErrorContains(
		t,
		i.Param(ProcParamPartial{MergeNestedStrategy: und.Defined("deep")}),
		"merge_nested",
	)
	assert.NilError(t, i.Param(ProcParamPartial{MergeStrategy: und.Defined("append")}))

	opts := paramOptions(t, ProcParamPartial{MergeStrategy: und.Defined("append")})
	assert.Equal(t, opts.Merge, MergeAppend)
	assert.Equal(t, opts.MergeNested, MergeNestedRecursive)
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
			name:     "ignore twice",
			src:      "package x\n\n//kama:partial ignore\n//kama:partial ignore\ntype A struct{}\n",
			is:       ErrDirective,
			contains: []string{"x.go:3:1", `"ignore ignore"`},
		},
		{
			name: "marked by a type group and ignored",
			src: "package x\n\n//kama:partial\ntype (\n\t//kama:partial ignore\n\tA struct{}\n" +
				"\tB struct{}\n)\n",
			is:       ErrDirective,
			contains: []string{"x.go:3:1", "both marked and ignored", "x.go:5:2"},
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
			name:     "unparsable args",
			src:      "package x\n\n//kama:partial ignore=\"x\ntype A struct{}\n",
			is:       genopt.ErrDirective,
			contains: []string{"x.go:3:1", "unterminated"},
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
			name: "merge on two lines",
			src: "package x\n\n//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:merge append\n\tB []int //kama:partial:merge overwrite\n}\n",
			is:       ErrDirective,
			contains: []string{"x.go:5:2", `"append overwrite"`, "want one strategy"},
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
			name: "append on an unknown member fallback",
			src: "package x\n\n//kama:partial\ntype A struct {\n" +
				"\t//kama:partial:merge append\n\tB map[string]any `json:\",embed\"`\n}\n",
			is:       ErrDirective,
			contains: []string{"x.go:5:2", "A.B", "want overwrite"},
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
			_, err := generate(t, map[string]string{"x.go": c.src}, testRun{})
			assert.ErrorIs(t, err, c.is)

			if errors.Is(err, genopt.ErrDirective) {
				assert.ErrorIs(t, err, ErrDirective)
			}

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

// TestTypeGroup checks that the directives in the doc comment of a type
// declaration with several specs apply to every type of the declaration.
func TestTypeGroup(t *testing.T) {
	files := generated(t, map[string]string{
		"a.go": `package x

//kama:partial
//kama:partial:fieldmask
type (
	A struct{ I int }
	B struct {
		//kama:partial:fieldmask name=j
		J int
	}
)

type (
	// C is not marked by the group of A and B.
	C struct{ K int }
)
`,
	})

	assert.DeepEqual(
		t,
		partialTypes(t, files["a.kama_partial.go"]),
		[]string{"APartial", "BPartial"},
	)

	fs := funcs(t, files["a.kama_partial.go"])
	assert.Assert(t, fs["APartial.FieldMask"] != "")
	assert.Assert(t, strings.Contains(fs["BPartial.FieldMask"], `"j"`), fs["BPartial.FieldMask"])
}

func TestGeneratedDeclsAreNotTargets(t *testing.T) {
	files := generated(t, map[string]string{
		"a.go": "// Package x is marked.\n//\n//kama:partial package\npackage x\n\ntype A struct{}\n",
		"a.kama_partial.go": "package x\n\n" +
			"//kama:generated kama:partial\ntype APartial struct{}\n\n" +
			"// Leftover is marked by a registered generator.\n//\n" +
			"//kama:generated kama:partial\ntype Leftover struct{}\n",
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

	got := opiter.WrapSeq2(maps.All(fieldTypes(t, src, "APartial"))).
		Map(withoutTag).
		Collect(maps.Collect)

	assert.DeepEqual(t, got, want)

	assert.Assert(t, strings.Contains(src, "type SPartial []und.Und[MPartial]"), src)
	assert.Assert(t, strings.Contains(src, "type OmPartial map[int]und.Und[M]"), src)
	assert.Assert(t, strings.Contains(src, "type GPartial[K comparable, V any] struct"), src)
}

// TestMappingFallback checks that an unknown member fallback keeps its type
// and its tag, and that a json tag with embed on any other field does not
// make a fallback.
func TestMappingFallback(t *testing.T) {
	files := generated(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct {\n" +
			"\tUnknown map[string]any `json:\",embed\" yaml:\"-\"`\n" +
			"\tNamed   Extra          `json:\",embed\"`\n" +
			"\tPlain   map[string]any `json:\"plain\"`\n" +
			"}\n\ntype Extra map[string]string\n",
	})

	assert.DeepEqual(t, fieldTypes(t, files["a.kama_partial.go"], "APartial"), map[string][2]string{
		"Unknown": {"map[string]any", "`json:\",embed\" yaml:\"-\"`"},
		"Named":   {"Extra", "`json:\",embed\"`"},
		"Plain":   {"und.Und[map[string]any]", "`json:\"plain,omitzero\"`"},
	})

	fs := funcs(t, files["a.kama_partial.go"])
	assert.Assert(t, strings.Contains(fs["APartial.FromValue"], "p.Unknown = v.Unknown"))
	assert.Assert(t, strings.Contains(fs["APartial.ToValue"], "v.Unknown = p.Unknown"))
	assert.Assert(
		t,
		strings.Contains(
			fs["APartial.Merge"],
			"if r.Unknown != nil {\n\t\tp.Unknown = r.Unknown\n\t}",
		),
		fs["APartial.Merge"],
	)
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
		assert.Assert(t, strings.Contains(src, "\n//kama:generated kama:partial\ntype "), src)
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
	assert.Equal(t, NewLoader(nil).PackageFile.(fileloader.PathInput).Base, naming.Name)
}
