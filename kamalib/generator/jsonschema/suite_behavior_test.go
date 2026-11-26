package jsonschema

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
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

	"github.com/ngicks/kama/kamalib/internal/suitetest"
	"github.com/ngicks/kama/kamalib/workflow"
)

// suiteKey locates a test of the suite.
type suiteKey struct {
	file, group, test string
}

// suiteGenerated is the least number of groups of each top-level file of
// the suite that Generate accepts. A file generating fewer means Generate
// rejects schemas it used to accept. A file missing here generates none.
var suiteGenerated = map[string]int{
	"additionalProperties.json":         9,
	"allOf.json":                        12,
	"anchor.json":                       9,
	"anyOf.json":                        8,
	"boolean_schema.json":               2,
	"const.json":                        17,
	"contains.json":                     20,
	"content.json":                      4,
	"default.json":                      3,
	"dependentRequired.json":            4,
	"dependentSchemas.json":             4,
	"dynamicRef.json":                   21,
	"enum.json":                         17,
	"format-date-time.json":             2,
	"format-date.json":                  2,
	"format-duration.json":              2,
	"format-email.json":                 2,
	"format-hostname.json":              2,
	"format-idn-email.json":             2,
	"format-idn-hostname.json":          2,
	"format-ipv4.json":                  2,
	"format-ipv6.json":                  2,
	"format-iri-reference.json":         2,
	"format-iri.json":                   2,
	"format-json-pointer.json":          2,
	"format-regex.json":                 2,
	"format-relative-json-pointer.json": 2,
	"format-time.json":                  2,
	"format-unknown.json":               1,
	"format-uri-reference.json":         2,
	"format-uri-template.json":          2,
	"format-uri.json":                   2,
	"format-uuid.json":                  2,
	"id.json":                           25,
	"if-then-else.json":                 12,
	"infinite-loop-detection.json":      1,
	"items.json":                        14,
	"maxItems.json":                     2,
	"maxLength.json":                    2,
	"maxProperties.json":                3,
	"maximum.json":                      3,
	"minItems.json":                     2,
	"minLength.json":                    2,
	"minProperties.json":                2,
	"minimum.json":                      3,
	"multipleOf.json":                   6,
	"not.json":                          9,
	"oneOf.json":                        11,
	"pattern.json":                      4,
	"patternProperties.json":            6,
	"properties.json":                   6,
	"propertyNames.json":                6,
	"ref.json":                          23,
	"required.json":                     5,
	"type.json":                         12,
	"unevaluatedItems.json":             29,
	"unevaluatedProperties.json":        44,
	"uniqueItems.json":                  6,
	"vocabulary.json":                   2,
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
	"format-date-time.json":                    1,
	"format-date.json":                         1,
	"format-duration.json":                     1,
	"format-email.json":                        2,
	"format-hostname.json":                     2,
	"format-idn-email.json":                    1,
	"format-idn-hostname.json":                 2,
	"format-ipv4.json":                         1,
	"format-ipv6.json":                         1,
	"format-iri-reference.json":                1,
	"format-iri.json":                          1,
	"format-json-pointer.json":                 1,
	"format-regex.json":                        1,
	"format-relative-json-pointer.json":        1,
	"format-time.json":                         1,
	"format-uri-reference.json":                1,
	"format-uri-template.json":                 1,
	"format-uri.json":                          1,
	"format-uuid.json":                         1,
	"optional/bignum.json":                     12,
	"optional/dependencies-compatibility.json": 7,
	"optional/dynamicRef.json":                 1,
	"optional/ecmascript-regex.json":           20,
	"optional/float-overflow.json":             1,
	"optional/format-assertion.json":           1,
	"optional/format-regex.json":               6,
	"optional/format-time.json":                1,
	"optional/non-bmp-regex.json":              2,
	"optional/non-schemas.json":                3,
}

// optionalUnsupported is the unsupported table of the optional set.
var optionalUnsupported = map[string]int{
	"optional/cross-draft.json":      2,
	"optional/format-assertion.json": 1,
	"optional/non-schemas.json":      5,
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
	"optional/bignum.json: enum comparison with high precision": "a number is float64, " +
		"which rounds the decimals to the value of the enum",
	"optional/bignum.json: const comparison with high precision": "a number is float64, " +
		"which rounds the decimals to the value of the const",
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
	"format-date-time.json: validation of date-time strings: " +
		"a valid date-time with a leap second, UTC": "time.RFC3339 rejects a leap second",
	"format-date-time.json: validation of date-time strings: " +
		"a valid date-time with a leap second, with minus offset": "time.RFC3339 rejects a leap second",
	"format-date-time.json: validation of date-time strings: " +
		"an invalid offset in date-time string": "time.RFC3339 accepts an offset hour out of range",
	"format-date-time.json: validation of date-time strings: " +
		"an invalid offset minute in date-time string": "time.RFC3339 accepts an offset minute " +
		"out of range",
	"format-date-time.json: validation of date-time strings: " +
		"case-insensitive T and Z": "time.RFC3339 rejects a lower-case t and z",
	"format-date-time.json: validation of date-time strings: " +
		"a comma decimal separator with a time-offset": "time.RFC3339 accepts a comma before " +
		"the fraction of a second",
	// A group of a top-level file is named rather than the file, whose
	// other groups belong to the main set and must agree.
	"format-duration.json: validation of duration strings": "format duration not asserted",
	"format-email.json: validation of e-mail addresses":    "format email not asserted",
	"format-email.json: " +
		"validation of e-mail addresses against the RFC 5321 Mailbox rule": "format email not asserted",
	"format-hostname.json: validation of host names": "format hostname not asserted",
	"format-hostname.json: " +
		"validation of A-label (punycode) host names": "format hostname not asserted",
	"format-idn-email.json: " +
		"validation of an internationalized e-mail addresses": "format idn-email not asserted",
	"format-idn-hostname.json: " +
		"validation of internationalized host names": "format idn-hostname not asserted",
	"format-idn-hostname.json: " +
		"validation of separators in internationalized host names": "format idn-hostname not asserted",
	"format-iri.json: validation of IRIs":                     "format iri not asserted",
	"format-iri-reference.json: validation of IRI References": "format iri-reference not asserted",
	"format-json-pointer.json: " +
		"validation of JSON-pointers (JSON String Representation)": "format json-pointer not asserted",
	"format-regex.json: validation of regular expressions": "format regex not asserted",
	"format-relative-json-pointer.json: " +
		"validation of Relative JSON Pointers (RJP)": "format relative-json-pointer not asserted",
	"format-time.json: validation of time strings":            "format time not asserted",
	"format-uri.json: validation of URIs":                     "format uri not asserted",
	"format-uri-reference.json: validation of URI References": "format uri-reference not asserted",
	"format-uri-template.json: validation of URI-Templates":   "format uri-template not asserted",
	"optional/format-regex.json":                              "format regex not asserted",
	"optional/format-time.json":                               "format time not asserted",
}

// suiteEnv names the environment variable that enables TestSuiteBehavior.
const suiteEnv = "KAMA_JSONSCHEMA_SUITE"

// suiteDir holds the generated suite fixtures. The repository ignores it,
// and its go.mod keeps it out of the patterns of this module, so a fixture
// that no longer compiles breaks no build but the suite run.
var suiteDir = filepath.Join("internal", "suite")

// suiteSet is a set of groups of the suite and the tables checking them.
type suiteSet struct {
	name string
	// selects reports whether a group belongs to the set. Each group of the
	// suite belongs to exactly one set.
	selects func(g suitetest.Group) bool
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

// suiteSets are the groups of the top-level files of the suite, which the
// generated code agrees with on every instance, and the optional groups:
// those of the optional directory and those expecting format to assert,
// which draft 2020-12 leaves to an implementation. A group expecting
// format to annotate belongs to neither under assert_formats, which
// changes that behavior.
var suiteSets = []suiteSet{
	{
		name: "main",
		selects: func(g suitetest.Group) bool {
			return path.Dir(g.File) == "." && !g.HasTag("format-assertion")
		},
		generated:   suiteGenerated,
		unsupported: suiteUnsupported,
		reasons:     suiteUnsupportedReasons,
	},
	{
		name: "optional",
		selects: func(g suitetest.Group) bool {
			return (path.Dir(g.File) == "optional" || g.HasTag("format-assertion")) &&
				!g.HasTag("format-annotation")
		},
		generated:     optionalGenerated,
		unsupported:   optionalUnsupported,
		reasons:       optionalUnsupportedReasons,
		exceptions:    optionalExceptions,
		assertFormats: true,
	},
}

// TestSuiteBehavior generates every group of suiteSets that Generate
// accepts as a fixture under suiteDir, through jsonschema.generate and
// validate.generate as runSuiteSpec runs them, and checks the instances of
// the suite against them. Each group
// of the suite that applies to draft 2020-12 belongs to exactly one set.
// Run it with
//
//	KAMA_JSONSCHEMA_SUITE=1 go test ./generator/jsonschema/ -run TestSuite
//
// The fixtures stay on disk for inspection. Group gi of file f.json, a path
// relative to suitetest.Dir and gi the index of the group in its tests
// array, is the package <f>/g<gi> of the module in suiteDir, whose file
// g<gi>.go names the schema f.json#/tests/<gi>/schema with
// //kama:option-file, generated with the root type Root.
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
		keys    []suiteKey
		valid   []bool
		driver  suiteDriver
		batches []suiteBatch
	)

	exceptions := map[string]string{}
	groups := suitetest.Groups(t, os.DirFS(moduleRoot))

	for _, g := range groups {
		sets := opiter.SlicesValues(suiteSets).
			Filter(func(set suiteSet) bool { return set.selects(g) }).
			Map(func(set suiteSet) string { return set.name }).
			CollectSlice()

		assert.Equal(t, len(sets), 1, "%s: %s: in sets %v", g.File, g.Description, sets)
	}

	for _, set := range suiteSets {
		maps.Copy(exceptions, set.exceptions)

		generated, unsupported, reasons := map[string]int{}, map[string]int{}, map[string]int{}

		for _, g := range groups {
			if !set.selects(g) {
				continue
			}

			err := generateSuite(g, set.assertFormats)
			if errors.Is(err, ErrUnsupported) || errors.Is(err, ErrConflict) {
				unsupported[g.File]++

				for _, r := range slices.Compact(slices.Sorted(slices.Values(rejectReasons(err)))) {
					reasons[r]++
				}

				continue
			}

			assert.NilError(t, err, "%s: %s", g.File, g.Description)

			batches = batchSuite(batches, g, set.assertFormats)
			generated[g.File]++

			for _, c := range g.Tests {
				keys = append(
					keys,
					suiteKey{file: g.File, group: g.Description, test: c.Description},
				)
				valid = append(valid, c.Valid)

				driver.add(g.File, g.Index, c.Data)
			}
		}

		checkGenerated(t, set.generated, generated)
		checkUnsupported(t, set, unsupported, reasons)

		t.Logf("%s: %d groups generated, %d unsupported", set.name,
			sumValues(generated), sumValues(unsupported))

		for _, reason := range slices.Sorted(maps.Keys(reasons)) {
			t.Logf("unsupported: %d groups: %s", reasons[reason], reason)
		}
	}

	runSuiteSpec(t, batches)

	writeFile(t, filepath.Join(suiteDir, "main.go"), driver.source())

	// Plain go vet runs structtag, which rejects a json tag on an
	// unexported field, among the checks the generated code must pass.
	suiteGo(t, "vet", "./...")

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

	located := opiter.SlicesValues(keys).
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

	return opiter.SlicesValues(keys).
		Filter(func(key string) bool { _, ok := exceptions[key]; return ok }).
		First().
		Unpack()
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
		opiter.MapsKeys(set.unsupported).
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
		return opiter.SlicesValues(j.Unwrap()).
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
	return opiter.MapsValues(m).
		Reduce(func(sum, n int) int { return sum + n }, 0)
}

// suiteStem returns the directory of the packages of file under suiteDir.
func suiteStem(file string) string { return strings.TrimSuffix(file, ".json") }

// suiteGroupDir returns the name of the directory and package of group gi.
func suiteGroupDir(gi int) string { return fmt.Sprintf("g%03d", gi) }

// suiteBatch is the groups of the suite one jsonschema.generate step of
// runSuiteSpec generates: groups of one set whose external schemas agree on
// every URI they share, so that one remotes param serves them all.
type suiteBatch struct {
	assertFormats bool
	groups        []suitetest.Group
}

// batchSuite adds g, a group of a set asserting formats as assertFormats
// tells, to the first batch of batches it agrees with, or to a new one.
func batchSuite(batches []suiteBatch, g suitetest.Group, assertFormats bool) []suiteBatch {
	i := slices.IndexFunc(batches, func(b suiteBatch) bool {
		return b.assertFormats == assertFormats && b.agrees(g)
	})
	if i < 0 {
		batches = append(batches, suiteBatch{assertFormats: assertFormats})
		i = len(batches) - 1
	}

	batches[i].groups = append(batches[i].groups, g)

	return batches
}

// agrees reports whether the external schemas of g equal those of the
// groups of b at every URI they share.
func (b suiteBatch) agrees(g suitetest.Group) bool {
	return !opiter.SlicesValues(b.groups).
		Any(func(o suitetest.Group) bool {
			return opiter.MapsAll(g.ExternalSchemas).
				Any(func(uri string, v jsontext.Value) bool {
					w, ok := o.ExternalSchemas[uri]
					return ok && !bytes.Equal(v, w)
				})
		})
}

// remotes returns the remotes param of b: the meta-schemas and each URI of
// the external schemas of its groups, mapped to the schema of the first
// group naming it.
func (b suiteBatch) remotes() []any {
	return opiter.SlicesValues(suiteMetaRemotes()).
		Concat(opiter.SlicesValues(b.groups).
			FlatMap(externalRemotes).
			Iter()).
		UniqueFunc(func(r Remote) string { return r.Uri }).
		Map(func(r Remote) any { return map[string]any{"uri": r.Uri, "file": r.File} }).
		CollectSlice()
}

// suiteGroupPath returns the directory of the package of g, slash-separated
// and relative to the root of this module.
func suiteGroupPath(g suitetest.Group) string {
	return path.Join(
		"generator/jsonschema",
		filepath.ToSlash(suiteDir),
		suiteStem(g.File),
		suiteGroupDir(g.Index),
	)
}

// runSuiteSpec generates the groups of batches through the procs, as a
// spec of a user does: it writes a package per group under suiteDir whose
// only file names the schema of the group inside its file of the suite,
// then runs a spec loading the packages of each batch, running
// jsonschema.generate over them with the param of the batch, and then
// validate.generate over every package.
//
// The overlay is rooted at this module, which holds both the suite and
// suiteDir, so that a package names the schema inside the suite. The
// packages under suiteDir belong to another module, which the go.work
// suiteModule writes joins to this one for the loads, and are copied down
// without a compile check: building the driver checks them.
func runSuiteSpec(t *testing.T, batches []suiteBatch) {
	t.Helper()

	ctx := context.Background()

	module, err := filepath.Abs(moduleRoot)
	assert.NilError(t, err)

	work, err := filepath.Abs(filepath.Join(suiteDir, "go.work"))
	assert.NilError(t, err)

	// The go.work of the repository does not list the module, and the
	// go command refuses -mod=mod in workspace mode.
	for k, v := range map[string]string{"GOWORK": work, "GOFLAGS": "", "GOPROXY": "off"} {
		t.Setenv(k, v)
	}

	steps := map[string]workflow.Step{
		"root": {Proc: "overlayfs.open", Param: map[string]any{"root": module}},
	}

	// Every step writes to root or reads it, so each one waits for the one
	// before.
	var prev workflow.Names

	for i, b := range batches {
		var patterns []any

		for _, g := range b.groups {
			dir := suiteGroupPath(g)
			name := suiteGroupDir(g.Index)
			// The schema lies inside this module, up from the package.
			schema := strings.Repeat("../", strings.Count(dir, "/")+1) + g.SchemaRef()

			writeFile(t, filepath.Join(module, filepath.FromSlash(dir), name+".go"), []byte(
				"//kama:option-file jsonschema="+schema+"\n\npackage "+name+"\n",
			))

			patterns = append(patterns, "./"+dir)
		}

		load, generate := fmt.Sprintf("load%02d", i), fmt.Sprintf("generate%02d", i)
		steps[load] = workflow.Step{
			Proc:  "packages.load",
			In:    workflow.Names{"root"},
			After: prev,
			Param: map[string]any{"patterns": patterns, "allow_type_errors": true},
		}
		steps[generate] = workflow.Step{
			Proc: "jsonschema.generate",
			In:   workflow.Names{load},
			Out:  workflow.Names{"root"},
			Param: map[string]any{
				"type_name":      "Root",
				"remotes":        b.remotes(),
				"assert_formats": b.assertFormats,
			},
		}
		prev = workflow.Names{generate}
	}

	steps["load"] = workflow.Step{
		Proc:  "packages.load",
		In:    workflow.Names{"root"},
		After: prev,
		Param: map[string]any{"patterns": []any{"./generator/jsonschema/" +
			filepath.ToSlash(suiteDir) + "/..."}},
	}
	steps["validate"] = workflow.Step{
		Proc: "validate.generate",
		In:   workflow.Names{"load"},
		Out:  workflow.Names{"root"},
	}
	steps["final"] = workflow.Step{
		Proc:  "overlayfs.copy_down",
		In:    workflow.Names{"root"},
		After: workflow.Names{"validate"},
		Param: map[string]any{"fast_ignore_compile_errors": true},
	}

	g, err := workflow.Spec{Version: workflow.SpecVersion, Steps: steps}.Build(
		ctx, workflow.DefaultRegistry,
	)
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

// suiteModule makes suiteDir an empty module that resolves the runtime
// module next to this module from its directory and requires package
// option, with a go.work joining it to this module and the runtime module
// for the loads of runSuiteSpec. Generated code imports nothing of this module, so the module of
// the fixtures does not require it. It writes go.mod before it removes the fixtures of an earlier
// run,
// so an interrupted run leaves no fixture inside this module.
func suiteModule(t *testing.T) {
	t.Helper()

	gomod, err := os.ReadFile(filepath.FromSlash("../../go.mod"))
	assert.NilError(t, err)

	// A go line below that of this module would need go mod tidy.
	goLine := opiter.StringsLines(string(gomod)).
		Filter(func(l string) bool { return strings.HasPrefix(l, "go ") }).
		First()
	assert.Assert(t, goLine.IsSome())

	// Generated code imports package option, which the fixtures require
	// at the version this module does.
	option := opiter.StringsLines(string(gomod)).
		Map(strings.TrimSpace).
		Filter(func(l string) bool { return strings.HasPrefix(l, "github.com/ngicks/option ") }).
		First()
	assert.Assert(t, option.IsSome())

	// The sums of this module cover every module the generated code
	// reaches.
	gosum, err := os.ReadFile(filepath.FromSlash("../../go.sum"))
	assert.NilError(t, err)

	writeFile(t, filepath.Join(suiteDir, "go.mod"), []byte("module "+suiteModulePath+"\n\n"+
		goLine.Value()+"\n"+
		"require (\n"+
		"\tgithub.com/ngicks/kama/kamaruntime v0.0.0\n"+
		"\t"+option.Value()+"\n"+
		")\n\n"+
		"replace github.com/ngicks/kama/kamaruntime => ../../../../../kamaruntime\n"))
	writeFile(t, filepath.Join(suiteDir, "go.sum"), gosum)
	writeFile(t, filepath.Join(suiteDir, "go.work"), []byte(goLine.Value()+"\n"+
		"use (\n\t.\n\t../../../..\n\t../../../../../kamaruntime\n)\n"))

	entries, err := os.ReadDir(suiteDir)
	assert.NilError(t, err)

	for _, e := range entries {
		if !slices.Contains([]string{"go.mod", "go.sum", "go.work"}, e.Name()) {
			assert.NilError(t, os.RemoveAll(filepath.Join(suiteDir, e.Name())))
		}
	}
}

// runSuiteDriver runs the driver of the module in suiteDir and returns its
// line per case.
func runSuiteDriver(t *testing.T) []string {
	t.Helper()

	return opiter.StringsLines(suiteGo(t, "run", ".")).
		Map(func(l string) string { return strings.TrimSuffix(l, "\n") }).
		CollectSlice()
}

// suiteGo runs the go command with args in suiteDir and returns its
// standard output. A failing command fails t with its standard error.
func suiteGo(t *testing.T, args ...string) string {
	t.Helper()

	cmd := exec.Command("go", args...)
	cmd.Dir = suiteDir

	// The go.work of the repository does not list the module.
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOPROXY=off")

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	out, err := cmd.Output()
	assert.NilError(t, err, "go %s: %s", strings.Join(args, " "), stderr.String())

	return string(out)
}

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()

	assert.NilError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	assert.NilError(t, os.WriteFile(path, content, 0o644))
}
