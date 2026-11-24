package jsonschema

import (
	"bytes"
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
	"defs.json":                    1,
	"default.json":                 3,
	"dependentRequired.json":       4,
	"dependentSchemas.json":        4,
	"dynamicRef.json":              21,
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
	"ref.json":                     36,
	"refRemote.json":               15,
	"required.json":                5,
	"type.json":                    11,
	"unevaluatedItems.json":        29,
	"unevaluatedProperties.json":   44,
	"uniqueItems.json":             6,
	"vocabulary.json":              2,
}

// suiteUnsupported is the number of groups of each top-level file of the
// suite that Generate rejects. A file missing here rejects none.
var suiteUnsupported = map[string]int{}

// suiteUnsupportedReasons lists every reason a group of the suite is
// rejected for, as rejectReasons spells it.
var suiteUnsupportedReasons = []string{}

// optionalGenerated is the generated table of the optional set of
// suiteSets.
var optionalGenerated = map[string]int{
	"optional/anchor.json":                       1,
	"optional/bignum.json":                       8,
	"optional/dependencies-compatibility.json":   7,
	"optional/dynamicRef.json":                   1,
	"optional/ecmascript-regex.json":             20,
	"optional/float-overflow.json":               1,
	"optional/format-assertion.json":             1,
	"optional/format/date-time.json":             1,
	"optional/format/date.json":                  1,
	"optional/format/duration.json":              1,
	"optional/format/ecmascript-regex.json":      6,
	"optional/format/email.json":                 1,
	"optional/format/hostname.json":              2,
	"optional/format/idn-email.json":             1,
	"optional/format/idn-hostname.json":          2,
	"optional/format/ipv4.json":                  1,
	"optional/format/ipv6.json":                  1,
	"optional/format/iri-reference.json":         1,
	"optional/format/iri.json":                   1,
	"optional/format/json-pointer.json":          1,
	"optional/format/regex.json":                 1,
	"optional/format/relative-json-pointer.json": 1,
	"optional/format/time.json":                  1,
	"optional/format/unknown.json":               1,
	"optional/format/uri-reference.json":         1,
	"optional/format/uri-template.json":          1,
	"optional/format/uri.json":                   1,
	"optional/format/uuid.json":                  1,
	"optional/id.json":                           1,
	"optional/no-schema.json":                    1,
	"optional/non-bmp-regex.json":                2,
	"optional/unknownKeyword.json":               1,
}

// optionalUnsupported is the unsupported table of the optional set.
var optionalUnsupported = map[string]int{
	"optional/cross-draft.json":         1,
	"optional/format-assertion.json":    1,
	"optional/refOfUnknownKeyword.json": 5,
}

// optionalUnsupportedReasons are the reasons of the optional set.
var optionalUnsupportedReasons = []string{
	"unsupported schema: meta-schema %q is not that of draft 2020-12",
	"unsupported schema: meta-schema %q requires vocabulary %q",
	"unsupported schema: reference %q names no schema",
}

// optionalExceptions are the exceptions of the optional set. A key is a
// file, a file and a group, or a file, a group and a test, as
// suiteKey.String spells them. A key naming a file or a group excuses only
// the instances the suite calls invalid, which a check the generated code
// leaves out accepts; an instance the generated code rejects although the
// suite calls it valid needs a key of its own.
var optionalExceptions = map[string]string{
	"optional/bignum.json: integer: a bignum is an integer": "an integer is int64, " +
		"which a bignum overflows",
	"optional/bignum.json: integer: a negative bignum is an integer": "an integer is int64, " +
		"which a bignum overflows",
	"optional/float-overflow.json: all integers are multiples of 0.5, if overflow is handled: " +
		"valid if optional overflow handling is implemented": "an integer is int64, " +
		"which 1e308 overflows",
	"optional/dependencies-compatibility.json": "dependencies is no keyword of draft 2020-12",
	"optional/ecmascript-regex.json":           "regexp is RE2, not ECMA 262",
	"optional/ecmascript-regex.json: ECMA 262 \\s matches whitespace: " +
		"Line tabulation matches": "regexp is RE2, whose \\s matches ASCII whitespace only",
	"optional/ecmascript-regex.json: ECMA 262 \\s matches whitespace: " +
		"latin-1 non-breaking-space matches": "regexp is RE2, whose \\s matches ASCII whitespace only",
	"optional/ecmascript-regex.json: ECMA 262 \\s matches whitespace: " +
		"zero-width whitespace matches": "regexp is RE2, whose \\s matches ASCII whitespace only",
	"optional/ecmascript-regex.json: ECMA 262 \\s matches whitespace: " +
		"paragraph separator matches (line terminator)": "regexp is RE2, whose \\s matches " +
		"ASCII whitespace only",
	"optional/ecmascript-regex.json: ECMA 262 \\s matches whitespace: " +
		"EM SPACE matches (Space_Separator)": "regexp is RE2, whose \\s matches ASCII whitespace only",
	"optional/format/date-time.json: validation of date-time strings: " +
		"a valid date-time with a leap second, UTC": "time.RFC3339 rejects a leap second",
	"optional/format/date-time.json: validation of date-time strings: " +
		"a valid date-time with a leap second, with minus offset": "time.RFC3339 rejects a leap second",
	"optional/format/date-time.json: validation of date-time strings: " +
		"an invalid offset in date-time string": "time.RFC3339 accepts an offset hour out of range",
	"optional/format/date-time.json: validation of date-time strings: " +
		"an invalid offset minute in date-time string": "time.RFC3339 accepts an offset minute " +
		"out of range",
	"optional/format/date-time.json: validation of date-time strings: " +
		"case-insensitive T and Z": "time.RFC3339 rejects a lower-case t and z",
	"optional/format/date-time.json: validation of date-time strings: " +
		"a comma decimal separator with a time-offset": "time.RFC3339 accepts a comma before " +
		"the fraction of a second",
	// The schemas of format/ecmascript-regex.json name the format regex.
	"optional/format/ecmascript-regex.json":      "format regex not asserted",
	"optional/format/duration.json":              "format duration not asserted",
	"optional/format/email.json":                 "format email not asserted",
	"optional/format/hostname.json":              "format hostname not asserted",
	"optional/format/idn-email.json":             "format idn-email not asserted",
	"optional/format/idn-hostname.json":          "format idn-hostname not asserted",
	"optional/format/iri.json":                   "format iri not asserted",
	"optional/format/iri-reference.json":         "format iri-reference not asserted",
	"optional/format/json-pointer.json":          "format json-pointer not asserted",
	"optional/format/regex.json":                 "format regex not asserted",
	"optional/format/relative-json-pointer.json": "format relative-json-pointer not asserted",
	"optional/format/time.json":                  "format time not asserted",
	"optional/format/uri.json":                   "format uri not asserted",
	"optional/format/uri-reference.json":         "format uri-reference not asserted",
	"optional/format/uri-template.json":          "format uri-template not asserted",
}

// suiteEnv names the environment variable that enables TestSuiteBehavior.
const suiteEnv = "KAMA_JSONSCHEMA_SUITE"

// suiteDir holds the generated suite fixtures. The repository ignores it,
// and its go.mod keeps it out of the patterns of this module, so a fixture
// that no longer compiles breaks no build but the suite run.
var suiteDir = filepath.Join("internal", "suite")

// suiteSet is a set of files of the suite and the tables checking them.
type suiteSet struct {
	// patterns match the files of the set, relative to suiteRoot.
	patterns []string
	// generated is the least number of groups of each file that Generate
	// accepts. A file generating fewer means Generate rejects schemas it
	// used to accept. A file missing here generates none.
	generated map[string]int
	// unsupported is the number of groups of each file that Generate
	// rejects. A file missing here rejects none.
	unsupported map[string]int
	// reasons lists every reason a group is rejected for, as rejectReasons
	// spells it.
	reasons []string
	// exceptions maps the instances on which the generated code disagrees
	// with the suite to the reason: a key names the instances of a file, of
	// a group or one instance, as suiteKey.String spells it, or its prefix
	// ending at the file or the group. Each key must name a disagreement.
	exceptions map[string]string
	// assertFormats is the assert_formats param of the set.
	assertFormats bool
}

// suiteSets are the top-level files of the suite, which the generated code
// agrees with on every instance, and the files of its optional directory.
var suiteSets = []suiteSet{
	{
		patterns:    []string{"*.json"},
		generated:   suiteGenerated,
		unsupported: suiteUnsupported,
		reasons:     suiteUnsupportedReasons,
	},
	{
		patterns:      []string{"optional/*.json", "optional/format/*.json"},
		generated:     optionalGenerated,
		unsupported:   optionalUnsupported,
		reasons:       optionalUnsupportedReasons,
		exceptions:    optionalExceptions,
		assertFormats: true,
	},
}

// suiteRoot holds the tests of draft 2020-12 of the suite.
var suiteRoot = filepath.FromSlash("../../pkg/jsonschema/testdata/suite/tests/draft2020-12")

// TestSuiteBehavior generates every group of the files of suiteSets that
// Generate accepts as a fixture under suiteDir, through the proc as a
// workflow spec runs it, and checks the instances of the suite against
// them. Run it with
//
//	KAMA_JSONSCHEMA_SUITE=1 go test ./generator/jsonschema/ -run TestSuite
//
// The fixtures stay on disk for inspection. Group gi of file f.json, a path
// relative to suiteRoot, is the package <f>/g<gi> of the module in suiteDir,
// generated from the schema f.json#/<gi>/schema with the root type Root.
//
// A driver program in suiteDir decodes each instance of a group into the
// root type of its package. A decode succeeds exactly when the suite calls
// the instance valid, unless the exceptions of the set name the instance,
// on which it must fail to. Each file generates at least the groups the
// set records, and rejects exactly the groups it records, each for reasons
// it lists.
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

	suiteModule(t)

	var (
		keys   []suiteKey
		valid  []bool
		driver suiteDriver
	)

	exceptions := map[string]string{}

	for _, set := range suiteSets {
		maps.Copy(exceptions, set.exceptions)

		generated, unsupported, reasons := map[string]int{}, map[string]int{}, map[string]int{}

		for _, file := range set.files(t) {
			b, err := os.ReadFile(filepath.Join(suiteRoot, filepath.FromSlash(file)))
			assert.NilError(t, err)

			var gs []suiteGroup
			assert.NilError(t, jsonv2.Unmarshal(b, &gs))

			var accepted []int

			for gi, g := range gs {
				err := generateSuite(g.Schema, set.assertFormats)
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
					keys = append(
						keys,
						suiteKey{file: file, group: g.Description, test: c.Description},
					)
					valid = append(valid, c.Valid)

					driver.add(file, gi, c.Data)
				}
			}

			runSuiteSpec(t, file, accepted, set.assertFormats)
		}

		checkGenerated(t, set.generated, generated)
		checkUnsupported(t, set, unsupported, reasons)

		t.Logf("%v: %d groups generated, %d unsupported", set.patterns,
			sumValues(generated), sumValues(unsupported))

		for _, reason := range slices.Sorted(maps.Keys(reasons)) {
			t.Logf("unsupported: %d groups: %s", reasons[reason], reason)
		}
	}

	runSuiteValidate(t)

	writeFile(t, filepath.Join(suiteDir, "main.go"), driver.source())

	results := runSuiteDriver(t)
	assert.Equal(t, len(results), len(keys))

	agree, excused := 0, map[string]int{}

	for i, k := range keys {
		if msg, ok := strings.CutPrefix(results[i], "E "); ok {
			t.Errorf("%s: round trip: %s", k, msg)
		}

		if got := results[i] != "0"; got == valid[i] {
			agree++
		} else if key, ok := k.exception(exceptions, valid[i]); ok {
			excused[key]++
		} else {
			t.Errorf("%s: valid is %t, decoding succeeded is %t", k, valid[i], got)
		}
	}

	for _, key := range slices.Sorted(maps.Keys(exceptions)) {
		if excused[key] == 0 {
			t.Errorf("an exception excuses no disagreement: %s: %s", key, exceptions[key])
		}
	}

	located := opiter.WrapSeq(slices.Values(keys)).
		Map(suiteKey.String).
		Collect(slices.Sorted)
	assert.Equal(t, len(slices.Compact(located)), len(keys), "a test of the suite is located twice")

	t.Logf("%d instances checked, %d agree, %d excepted", len(keys), agree, sumValues(excused))
}

// String spells k as the exceptions of a suiteSet key it.
func (k suiteKey) String() string { return k.file + ": " + k.group + ": " + k.test }

// exception returns the key of exceptions naming k, an instance the suite
// calls valid as valid tells: k itself, or its group or its file for an
// invalid one.
func (k suiteKey) exception(exceptions map[string]string, valid bool) (string, bool) {
	keys := []string{k.String()}
	if !valid {
		keys = append(keys, k.file+": "+k.group, k.file)
	}

	return opiter.WrapSeq(slices.Values(keys)).
		Filter(func(key string) bool { _, ok := exceptions[key]; return ok }).
		First().
		Unpack()
}

// files returns the files of the set, slash-separated and relative to
// suiteRoot.
func (s suiteSet) files(t *testing.T) []string {
	t.Helper()

	var files []string

	for _, p := range s.patterns {
		matches, err := filepath.Glob(filepath.Join(suiteRoot, filepath.FromSlash(p)))
		assert.NilError(t, err)

		for _, m := range matches {
			rel, err := filepath.Rel(suiteRoot, m)
			assert.NilError(t, err)

			files = append(files, filepath.ToSlash(rel))
		}
	}

	return files
}

// checkGenerated reports a file of the suite that generates fewer groups
// than want records, and logs one that generates more.
func checkGenerated(t *testing.T, want, generated map[string]int) {
	t.Helper()

	for _, file := range slices.Sorted(maps.Keys(want)) {
		if generated[file] < want[file] {
			t.Errorf("%s: %d groups generated, want at least %d",
				file, generated[file], want[file])
		}
	}

	for _, file := range slices.Sorted(maps.Keys(generated)) {
		if generated[file] > want[file] {
			t.Logf("%s: %d groups generated, more than %d: raise the number of the set",
				file, generated[file], want[file])
		}
	}
}

// checkUnsupported reports a file of the suite that rejects another number
// of groups than set records, a reason of a rejection that set does not
// list and a listed reason no rejection has.
func checkUnsupported(t *testing.T, set suiteSet, unsupported, reasons map[string]int) {
	t.Helper()

	files := slices.Compact(
		opiter.WrapSeq(maps.Keys(set.unsupported)).
			Concat(maps.Keys(unsupported)).
			Collect(slices.Sorted),
	)

	for _, file := range files {
		if unsupported[file] != set.unsupported[file] {
			t.Errorf("%s: %d groups unsupported, the set records %d",
				file, unsupported[file], set.unsupported[file])
		}
	}

	for _, r := range slices.Sorted(maps.Keys(reasons)) {
		if !slices.Contains(set.reasons, r) {
			t.Errorf("unsupported for a reason the set does not list: %s", r)
		}
	}

	for _, r := range set.reasons {
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

// suiteStem returns the directory of the packages of file under suiteDir.
func suiteStem(file string) string { return strings.TrimSuffix(file, ".json") }

// suiteGroupDir returns the name of the directory and package of group gi.
func suiteGroupDir(gi int) string { return fmt.Sprintf("g%03d", gi) }

// runSuiteSpec generates the groups accepted of file into suiteDir with a
// workflow running one jsonschema.generate step per group, asserting
// formats as assertFormats tells. The overlay is
// rooted at this module, which holds both the suite and suiteDir. The
// copy down skips its compile check: the packages under suiteDir belong
// to another module, and building the driver checks them.
func runSuiteSpec(t *testing.T, file string, accepted []int, assertFormats bool) {
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

	remotes := opiter.WrapSeq(slices.Values(suiteRemotes())).
		Map(func(r Remote) any { return map[string]any{"uri": r.Uri, "dir": r.Dir, "file": r.File} }).
		CollectSlice()

	var names workflow.Names

	for _, gi := range accepted {
		name := suiteGroupDir(gi)
		names = append(names, name)

		steps[name] = workflow.Step{
			Proc: "jsonschema.generate",
			Out:  workflow.Names{"root"},
			Param: map[string]any{
				"schema":         fmt.Sprintf("%s#/%d/schema", schema, gi),
				"dir":            path.Join(out, name),
				"type_name":      "Root",
				"remotes":        remotes,
				"assert_formats": assertFormats,
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
		"\t\"fmt\"\n\t\"reflect\"\n\t\"strconv\"\n\n")

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
// Validate reports. The decoder of any other type validates what it
// decodes, a nested union holding its unknown member included, so such a
// decoded value failing its Validate is an error of the generated code.
// Deterministic sorts the members of a map, which would otherwise make two
// encodings of one value differ.
const driverCheck = `func check[T any](b []byte) string {
	var v T
	if json.Unmarshal(b, &v) != nil {
		return "0"
	}

	if x, ok := any(v).(interface{ Validate(bool) error }); ok {
		if err := x.Validate(false); err != nil && isUnion(v) {
			return "0"
		} else if err != nil {
			return "E " + strconv.Quote("decoded a value Validate rejects: "+err.Error())
		}
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

// isUnion reports whether v is the holder of a union, whose field Kind holds
// its member.
func isUnion(v any) bool {
	t := reflect.TypeOf(v)
	if t.Kind() != reflect.Struct {
		return false
	}

	f, ok := t.FieldByName("Kind")

	return ok && f.Type.Kind() == reflect.Interface
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
