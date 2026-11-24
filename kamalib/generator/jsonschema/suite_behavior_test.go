package jsonschema

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"iter"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/ngicks/opiter"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/workflow"
)

// suiteCase is a test of a group of the suite: an instance and whether
// the schema of the group accepts it.
type suiteCase struct {
	Description string         `json:"description"`
	Data        jsontext.Value `json:"data"`
	Valid       bool           `json:"valid"`
}

// suiteKey locates a test of the suite.
type suiteKey struct {
	file, group, test string
}

// suiteGenerated is the least number of groups of each top-level file of
// the suite that Generate accepts. A file generating fewer means Generate
// rejects schemas it used to accept. A file missing here generates none.
var suiteGenerated = map[string]int{
	"additionalProperties.json":    9,
	"allOf.json":                   12,
	"anchor.json":                  4,
	"anyOf.json":                   8,
	"boolean_schema.json":          2,
	"const.json":                   17,
	"contains.json":                7,
	"content.json":                 4,
	"default.json":                 3,
	"dependentRequired.json":       4,
	"dependentSchemas.json":        4,
	"dynamicRef.json":              1,
	"enum.json":                    16,
	"exclusiveMaximum.json":        1,
	"exclusiveMinimum.json":        1,
	"format.json":                  19,
	"if-then-else.json":            12,
	"infinite-loop-detection.json": 1,
	"items.json":                   10,
	"maxContains.json":             5,
	"maxItems.json":                2,
	"maxLength.json":               2,
	"maxProperties.json":           3,
	"maximum.json":                 2,
	"minContains.json":             8,
	"minItems.json":                2,
	"minLength.json":               2,
	"minProperties.json":           2,
	"minimum.json":                 2,
	"multipleOf.json":              6,
	"not.json":                     9,
	"oneOf.json":                   11,
	"pattern.json":                 3,
	"patternProperties.json":       6,
	"prefixItems.json":             4,
	"properties.json":              6,
	"propertyNames.json":           6,
	"ref.json":                     35,
	"required.json":                5,
	"type.json":                    11,
	"unevaluatedItems.json":        28,
	"unevaluatedProperties.json":   43,
	"uniqueItems.json":             6,
}

// suiteUnsupported is the number of groups of each top-level file of the
// suite that Generate rejects. A file missing here rejects none.
var suiteUnsupported = map[string]int{
	"defs.json":                  1,
	"dynamicRef.json":            20,
	"ref.json":                   1,
	"refRemote.json":             15,
	"unevaluatedItems.json":      1,
	"unevaluatedProperties.json": 1,
	"vocabulary.json":            2,
}

// suiteUnsupportedReasons lists every reason a group of the suite is
// rejected for, as rejectReasons spells it.
var suiteUnsupportedReasons = []string{
	"unsupported schema: keyword $dynamicRef is not supported",
	"unsupported schema: meta-schema %q is not that of draft 2020-12",
	"unsupported schema: reference %q leaves the document",
}

// suiteEnv names the environment variable that enables TestSuiteBehavior.
const suiteEnv = "KAMA_JSONSCHEMA_SUITE"

// suiteDir holds the generated suite fixtures. The repository ignores it,
// and its go.mod keeps it out of the patterns of this module, so a fixture
// that no longer compiles breaks no build but the suite run.
var suiteDir = filepath.Join("internal", "suite")

// TestSuiteBehavior generates every group of the top-level files of the
// draft 2020-12 suite that Generate accepts as a fixture under suiteDir,
// through the proc as a workflow spec runs it, and checks the instances
// of the suite against them. Run it with
//
//	KAMA_JSONSCHEMA_SUITE=1 go test ./generator/jsonschema/ -run TestSuite
//
// The fixtures stay on disk for inspection. Group gi of file f.json is the
// package <f>/g<gi> of the module in suiteDir, generated from the schema
// f.json#/<gi>/schema with the root type Root.
//
// A driver program in suiteDir decodes each instance of a group into the
// root type of its package. A decode succeeds exactly when the suite calls
// the instance valid. Each file generates at least the groups suiteGenerated
// records, and rejects exactly the groups suiteUnsupported records, each
// for reasons of suiteUnsupportedReasons.
//
// The driver encodes every value that decodes, decodes the result and
// encodes it again. The second decode must succeed and both encodings must
// be equal. They are not compared with the instance, which decoding may
// change by design: it drops unknown members, an anyOf keeps the first
// member that matches, and 1.0 encodes as 1.
func TestSuiteBehavior(t *testing.T) {
	if os.Getenv(suiteEnv) != "1" {
		t.Skipf("set %s=1 to generate and check the suite fixtures", suiteEnv)
	}

	if testing.Short() {
		t.Skip("builds a program from the generated suite fixtures")
	}

	root := filepath.FromSlash("../../pkg/jsonschema/testdata/suite/tests/draft2020-12")

	files, err := filepath.Glob(filepath.Join(root, "*.json"))
	assert.NilError(t, err)

	suiteModule(t)

	var (
		keys   []suiteKey
		valid  []bool
		driver suiteDriver
	)

	generated, unsupported, reasons := map[string]int{}, map[string]int{}, map[string]int{}

	for _, path := range files {
		b, err := os.ReadFile(path)
		assert.NilError(t, err)

		var gs []suiteGroup
		assert.NilError(t, jsonv2.Unmarshal(b, &gs))

		file := filepath.Base(path)

		var accepted []int

		for gi, g := range gs {
			err := generateSuite(g.Schema)
			if errors.Is(err, ErrUnsupported) || errors.Is(err, ErrConflict) {
				unsupported[file]++

				for _, r := range slices.Compact(slices.Sorted(slices.Values(rejectReasons(err)))) {
					reasons[r]++
				}

				continue
			}

			assert.NilError(t, err, "%s: %s", file, g.Description)

			accepted = append(accepted, gi)
			generated[file]++

			for _, c := range g.Tests {
				keys = append(keys, suiteKey{file: file, group: g.Description, test: c.Description})
				valid = append(valid, c.Valid)

				driver.add(file, gi, c.Data)
			}
		}

		runSuiteSpec(t, file, accepted)
	}

	checkGenerated(t, generated)
	checkUnsupported(t, unsupported, reasons)

	runSuiteValidate(t)

	writeFile(t, filepath.Join(suiteDir, "main.go"), driver.source())

	results := runSuiteDriver(t)
	assert.Equal(t, len(results), len(keys))

	agree := 0

	for i, k := range keys {
		if msg, ok := strings.CutPrefix(results[i], "E "); ok {
			t.Errorf("%s: %s: %s: round trip: %s", k.file, k.group, k.test, msg)
		}

		if got := results[i] != "0"; got != valid[i] {
			t.Errorf("%s: %s: %s: valid is %t, decoding succeeded is %t",
				k.file, k.group, k.test, valid[i], got)

			continue
		}

		agree++
	}

	assert.Equal(t, len(slices.Compact(slices.SortedFunc(slices.Values(keys), compareSuiteKeys))),
		len(keys), "a test of the suite is located twice")

	t.Logf("%d groups generated, %d unsupported; %d instances checked, %d agree",
		sumValues(generated), sumValues(unsupported), len(keys), agree)

	for _, reason := range slices.Sorted(maps.Keys(reasons)) {
		t.Logf("unsupported: %d groups: %s", reasons[reason], reason)
	}
}

// checkGenerated reports a file of the suite that generates fewer groups
// than suiteGenerated records, and logs one that generates more.
func checkGenerated(t *testing.T, generated map[string]int) {
	t.Helper()

	for _, file := range slices.Sorted(maps.Keys(suiteGenerated)) {
		if generated[file] < suiteGenerated[file] {
			t.Errorf("%s: %d groups generated, want at least %d",
				file, generated[file], suiteGenerated[file])
		}
	}

	for _, file := range slices.Sorted(maps.Keys(generated)) {
		if generated[file] > suiteGenerated[file] {
			t.Logf("%s: %d groups generated, more than %d: raise the number in suiteGenerated",
				file, generated[file], suiteGenerated[file])
		}
	}
}

// checkUnsupported reports a file of the suite that rejects another number
// of groups than suiteUnsupported records, a reason of a rejection that
// suiteUnsupportedReasons does not list and a listed reason no rejection
// has.
func checkUnsupported(t *testing.T, unsupported, reasons map[string]int) {
	t.Helper()

	files := slices.Compact(
		opiter.WrapSeq(maps.Keys(suiteUnsupported)).
			Concat(maps.Keys(unsupported)).
			Collect(slices.Sorted),
	)

	for _, file := range files {
		if unsupported[file] != suiteUnsupported[file] {
			t.Errorf("%s: %d groups unsupported, suiteUnsupported records %d",
				file, unsupported[file], suiteUnsupported[file])
		}
	}

	for _, r := range slices.Sorted(maps.Keys(reasons)) {
		if !slices.Contains(suiteUnsupportedReasons, r) {
			t.Errorf("unsupported for a reason suiteUnsupportedReasons does not list: %s", r)
		}
	}

	for _, r := range suiteUnsupportedReasons {
		if reasons[r] == 0 {
			t.Errorf("no group is unsupported for the listed reason: %s", r)
		}
	}
}

// rejectReasons returns the reason of each error err joins. A reason is
// the message of the error without the pointer it names and with every
// quoted string replaced by %q, so groups rejected alike share it. An
// unsupported keyword is named in the reason.
func rejectReasons(err error) []string {
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		return opiter.WrapSeq(slices.Values(j.Unwrap())).
			FlatMap(func(e error) iter.Seq[string] { return slices.Values(rejectReasons(e)) }).
			CollectSlice()
	}

	msg := err.Error()

	for _, sentinel := range []error{ErrUnsupported, ErrConflict} {
		rest, ok := strings.CutPrefix(msg, sentinel.Error()+": ")
		if !ok {
			continue
		}

		// A conflict names two pointers in its message and none before it.
		ptr, text, ok := strings.Cut(rest, ": ")
		if !ok {
			ptr, text = "", rest
		}

		text = quotedString.ReplaceAllString(text, "%q")

		if text == "keyword is not supported" {
			text = "keyword " + ptr[strings.LastIndex(ptr, "/")+1:] + " is not supported"
		}

		return []string{strings.TrimPrefix(sentinel.Error(), "jsonschema: ") + ": " + text}
	}

	return []string{msg}
}

var quotedString = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)

func sumValues(m map[string]int) int {
	return opiter.WrapSeq(maps.Values(m)).
		Reduce(func(sum, n int) int { return sum + n }, 0)
}

func compareSuiteKeys(a, b suiteKey) int {
	return cmp.Or(cmp.Compare(a.file, b.file), cmp.Compare(a.group, b.group),
		cmp.Compare(a.test, b.test))
}

// suiteStem returns the directory of the packages of file under suiteDir.
func suiteStem(file string) string { return strings.TrimSuffix(file, ".json") }

// suiteGroupDir returns the name of the directory and package of group gi.
func suiteGroupDir(gi int) string { return fmt.Sprintf("g%03d", gi) }

// runSuiteSpec generates the groups accepted of file into suiteDir with a
// workflow running one jsonschema.generate step per group. The overlay is
// rooted at this module, which holds both the suite and suiteDir. The
// copy down skips its compile check: the packages under suiteDir belong
// to another module, and building the driver checks them.
func runSuiteSpec(t *testing.T, file string, accepted []int) {
	t.Helper()

	if len(accepted) == 0 {
		return
	}

	ctx := context.Background()

	module, err := filepath.Abs("../..")
	assert.NilError(t, err)

	out := path.Join("generator/jsonschema", filepath.ToSlash(suiteDir), suiteStem(file))
	schema := "pkg/jsonschema/testdata/suite/tests/draft2020-12/" + file

	steps := map[string]workflow.Step{
		"root": {Proc: "overlayfs.open", Param: map[string]any{"root": module}},
	}

	var names workflow.Names

	for _, gi := range accepted {
		name := suiteGroupDir(gi)
		names = append(names, name)

		steps[name] = workflow.Step{
			Proc: "jsonschema.generate",
			Out:  workflow.Names{"root"},
			Param: map[string]any{
				"schema":    fmt.Sprintf("%s#/%d/schema", schema, gi),
				"dir":       path.Join(out, name),
				"type_name": "Root",
			},
		}
	}

	steps["final"] = workflow.Step{
		Proc:  "overlayfs.copy_down",
		In:    workflow.Names{"root"},
		After: names,
		Param: map[string]any{"fast_ignore_compile_errors": true},
	}

	spec := workflow.Spec{Version: workflow.SpecVersion, Steps: steps}

	g, err := spec.Build(ctx, workflow.DefaultRegistry)
	assert.NilError(t, err, file)
	assert.Equal(t, len(g.Warnings()), 0, "%s: %v", file, g.Warnings())
	assert.NilError(t, g.Run(ctx, workflow.ProgressControl{}), file)
}

// runSuiteValidate runs validate.generate over the packages of the module
// in suiteDir, which the generated decoders call the Validate methods of.
// The overlay is rooted at suiteDir, since the go command loads the
// packages of a module from its own directory only.
func runSuiteValidate(t *testing.T) {
	t.Helper()

	// The go.work of the repository does not list the module.
	for k, v := range map[string]string{"GOWORK": "off", "GOFLAGS": "-mod=mod", "GOPROXY": "off"} {
		t.Setenv(k, v)
	}

	ctx := context.Background()

	dir, err := filepath.Abs(suiteDir)
	assert.NilError(t, err)

	spec := workflow.Spec{Version: workflow.SpecVersion, Steps: map[string]workflow.Step{
		"root": {Proc: "overlayfs.open", Param: map[string]any{"root": dir}},
		"load": {Proc: "packages.load", In: workflow.Names{"root"}},
		"validate": {
			Proc: "validate.generate",
			In:   workflow.Names{"load"},
			Out:  workflow.Names{"root"},
		},
		"final": {
			Proc:  "overlayfs.copy_down",
			In:    workflow.Names{"root"},
			After: workflow.Names{"validate"},
			Param: map[string]any{"fast_ignore_compile_errors": true},
		},
	}}

	g, err := spec.Build(ctx, workflow.DefaultRegistry)
	assert.NilError(t, err)
	assert.Equal(t, len(g.Warnings()), 0, "%v", g.Warnings())
	assert.NilError(t, g.Run(ctx, workflow.ProgressControl{}))
}

// suiteDriver collects the decodes the driver program runs.
type suiteDriver struct {
	imports []string
	cases   []string
}

func (d *suiteDriver) add(file string, gi int, data jsontext.Value) {
	alias := suitePackage(file, gi)

	imp := fmt.Sprintf("\t%s %q\n", alias,
		suiteModulePath+"/"+suiteStem(file)+"/"+suiteGroupDir(gi))
	if !slices.Contains(d.imports, imp) {
		d.imports = append(d.imports, imp)
	}

	d.cases = append(
		d.cases,
		fmt.Sprintf("\t{check[%s.Root], %s},\n", alias, strconv.Quote(string(data))),
	)
}

// source returns the driver program. It prints one line per case: 0 for
// an instance that does not decode, 1 for one that decodes and survives the
// round trip, and E followed by the quoted reason for one that decodes and
// fails it.
func (d *suiteDriver) source() []byte {
	var b bytes.Buffer

	b.WriteString("package main\n\nimport (\n\t\"bytes\"\n\t\"encoding/json/v2\"\n" +
		"\t\"fmt\"\n\t\"strconv\"\n\n")

	for _, imp := range d.imports {
		b.WriteString(imp)
	}

	b.WriteString(")\n\n")
	b.WriteString(driverCheck)
	b.WriteString("var cases = []struct {\n\tcheck func([]byte) string\n\tdata  string\n}{\n")

	for _, c := range d.cases {
		b.WriteString(c)
	}

	b.WriteString("}\n\n")
	b.WriteString("func main() {\n\tfor _, c := range cases {\n" +
		"\t\tfmt.Println(c.check([]byte(c.data)))\n\t}\n}\n")

	return b.Bytes()
}

// suitePackage names the import of group gi of file in the driver after
// both, so a failing build points to the group.
func suitePackage(file string, gi int) string {
	name := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}

		return '_'
	}, suiteStem(file))

	return name + "_" + strconv.Itoa(gi)
}

// driverCheck is the function of the driver deciding the line of a case. A
// value decodes when Unmarshal succeeds and the value passes its Validate,
// if any: a union yields its unknown member rather than failing, which only
// Validate reports.
// Deterministic sorts the members of a map, which would otherwise make two
// encodings of one value differ.
const driverCheck = `func check[T any](b []byte) string {
	var v T
	if json.Unmarshal(b, &v) != nil {
		return "0"
	}

	if x, ok := any(v).(interface{ Validate(bool) error }); ok && x.Validate(false) != nil {
		return "0"
	}

	first, err := json.Marshal(v, json.Deterministic(true))
	if err != nil {
		return "E " + strconv.Quote("encode: "+err.Error())
	}

	var w T
	if err := json.Unmarshal(first, &w); err != nil {
		return "E " + strconv.Quote("decode "+string(first)+": "+err.Error())
	}

	second, err := json.Marshal(w, json.Deterministic(true))
	if err != nil {
		return "E " + strconv.Quote("encode again: "+err.Error())
	}

	if !bytes.Equal(first, second) {
		return "E " + strconv.Quote(string(first)+" encodes again as "+string(second))
	}

	return "1"
}

`

const suiteModulePath = "example.invalid/suite"

// suiteModule makes suiteDir an empty module that resolves this module
// from its directory. It writes go.mod before it removes the fixtures of an
// earlier run, so an interrupted run leaves no fixture inside this module.
func suiteModule(t *testing.T) {
	t.Helper()

	gomod, err := os.ReadFile(filepath.FromSlash("../../go.mod"))
	assert.NilError(t, err)

	// A go line below that of this module would need go mod tidy.
	goLine := opiter.WrapSeq(strings.Lines(string(gomod))).
		Filter(func(l string) bool { return strings.HasPrefix(l, "go ") }).
		First()
	assert.Assert(t, goLine.IsSome())

	// The sums of this module cover every module the generated code
	// reaches through it.
	gosum, err := os.ReadFile(filepath.FromSlash("../../go.sum"))
	assert.NilError(t, err)

	writeFile(t, filepath.Join(suiteDir, "go.mod"), []byte("module "+suiteModulePath+"\n\n"+
		goLine.Value()+"\n"+
		"require github.com/ngicks/kama/kamalib v0.0.0\n\n"+
		"replace github.com/ngicks/kama/kamalib => ../../../..\n"))
	writeFile(t, filepath.Join(suiteDir, "go.sum"), gosum)

	entries, err := os.ReadDir(suiteDir)
	assert.NilError(t, err)

	for _, e := range entries {
		if e.Name() != "go.mod" && e.Name() != "go.sum" {
			assert.NilError(t, os.RemoveAll(filepath.Join(suiteDir, e.Name())))
		}
	}
}

// runSuiteDriver runs the driver of the module in suiteDir and returns its
// line per case.
func runSuiteDriver(t *testing.T) []string {
	t.Helper()

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = suiteDir

	// The go.work of the repository does not list the module.
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOPROXY=off")

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	out, err := cmd.Output()
	assert.NilError(t, err, "%s", stderr.String())

	return opiter.WrapSeq(strings.Lines(string(out))).
		Map(func(l string) string { return strings.TrimSuffix(l, "\n") }).
		CollectSlice()
}

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()

	assert.NilError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	assert.NilError(t, os.WriteFile(path, content, 0o644))
}
