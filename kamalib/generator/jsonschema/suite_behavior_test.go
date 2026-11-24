package jsonschema

import (
	"bytes"
	"cmp"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/ngicks/opiter"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
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

// The reasons a decode of generated code may disagree with the verdict of
// the suite. checkAllowed tells what each reason needs of the test.
const (
	// The generated code does not check the pure validation keywords. The
	// schema must hold one of unenforcedKeywords.
	reasonUnenforced = "a pure validation keyword is not checked"
	// A schema without type takes the kind its keywords imply, where the
	// suite accepts every other kind. The suite must call the instance
	// valid.
	reasonImpliedKind = "a schema without type takes the kind its keywords imply"
)

var unenforcedKeywords = []string{
	"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf",
	"minLength", "maxLength", "pattern", "format", "minItems", "maxItems",
	"uniqueItems", "minProperties", "maxProperties", "dependentRequired",
	"contentEncoding", "contentMediaType", "contentSchema",
}

// suiteAllowed lists the tests the generated code disagrees with by
// design, with the reason.
var suiteAllowed = map[suiteKey]string{
	{"anyOf.json", "anyOf", "neither anyOf valid"}: reasonUnenforced,
	{
		"default.json",
		"the default keyword does not do anything if the property is missing",
		"an explicit property value is checked against maximum (failing)",
	}: reasonUnenforced,
	{"dependentRequired.json", "single dependency", "missing dependency"}: reasonUnenforced,
	{
		"dependentRequired.json",
		"multiple dependents required",
		"missing dependency",
	}: reasonUnenforced,
	{
		"dependentRequired.json",
		"multiple dependents required",
		"missing other dependency",
	}: reasonUnenforced,
	{
		"dependentRequired.json",
		"multiple dependents required",
		"missing both dependencies",
	}: reasonUnenforced,
	{
		"dependentRequired.json",
		"dependencies with escaped characters",
		"CRLF missing dependent",
	}: reasonUnenforced,
	{
		"dependentRequired.json",
		"dependencies with escaped characters",
		"quoted quotes missing dependent",
	}: reasonUnenforced,
	{
		"exclusiveMaximum.json",
		"exclusiveMaximum validation",
		"boundary point is invalid",
	}: reasonUnenforced,
	{
		"exclusiveMaximum.json",
		"exclusiveMaximum validation",
		"above the exclusiveMaximum is invalid",
	}: reasonUnenforced,
	{
		"exclusiveMinimum.json",
		"exclusiveMinimum validation",
		"boundary point is invalid",
	}: reasonUnenforced,
	{
		"exclusiveMinimum.json",
		"exclusiveMinimum validation",
		"below the exclusiveMinimum is invalid",
	}: reasonUnenforced,
	{"items.json", "a schema given for items", "ignores non-arrays"}: reasonImpliedKind,
	{
		"items.json",
		"a schema given for items",
		"JavaScript pseudo-array is valid",
	}: reasonImpliedKind,
	{"maxItems.json", "maxItems validation", "too long is invalid"}: reasonUnenforced,
	{
		"maxItems.json",
		"maxItems validation with a decimal",
		"too long is invalid",
	}: reasonUnenforced,
	{"maxLength.json", "maxLength validation", "too long is invalid"}: reasonUnenforced,
	{
		"maxLength.json",
		"maxLength validation with a decimal",
		"too long is invalid",
	}: reasonUnenforced,
	{"maxProperties.json", "maxProperties validation", "too long is invalid"}: reasonUnenforced,
	{
		"maxProperties.json",
		"maxProperties validation with a decimal",
		"too long is invalid",
	}: reasonUnenforced,
	{
		"maxProperties.json",
		"maxProperties = 0 means the object is empty",
		"one property is invalid",
	}: reasonUnenforced,
	{"maximum.json", "maximum validation", "above the maximum is invalid"}: reasonUnenforced,
	{
		"maximum.json",
		"maximum validation with unsigned integer",
		"above the maximum is invalid",
	}: reasonUnenforced,
	{"minItems.json", "minItems validation", "too short is invalid"}: reasonUnenforced,
	{
		"minItems.json",
		"minItems validation with a decimal",
		"too short is invalid",
	}: reasonUnenforced,
	{"minLength.json", "minLength validation", "too short is invalid"}: reasonUnenforced,
	{
		"minLength.json",
		"minLength validation",
		"one grapheme is not long enough",
	}: reasonUnenforced,
	{
		"minLength.json",
		"minLength validation with a decimal",
		"too short is invalid",
	}: reasonUnenforced,
	{"minProperties.json", "minProperties validation", "too short is invalid"}: reasonUnenforced,
	{
		"minProperties.json",
		"minProperties validation with a decimal",
		"too short is invalid",
	}: reasonUnenforced,
	{"minimum.json", "minimum validation", "below the minimum is invalid"}: reasonUnenforced,
	{
		"minimum.json",
		"minimum validation with signed integer",
		"float below the minimum is invalid",
	}: reasonUnenforced,
	{
		"minimum.json",
		"minimum validation with signed integer",
		"int below the minimum is invalid",
	}: reasonUnenforced,
	{"multipleOf.json", "by int", "int by int fail"}:                            reasonUnenforced,
	{"multipleOf.json", "by number", "35 is not multiple of 1.5"}:               reasonUnenforced,
	{"multipleOf.json", "by small number", "0.00751 is not multiple of 0.0001"}: reasonUnenforced,
	{
		"multipleOf.json",
		"by very small number",
		"1.5e-8 is not multiple of 1e-8",
	}: reasonUnenforced,
	{"pattern.json", "pattern validation", "a non-matching pattern is invalid"}: reasonUnenforced,
	{
		"pattern.json",
		"pattern with Unicode property escape requires unicode mode",
		"Digits do not match",
	}: reasonUnenforced,
	{"properties.json", "object properties validation", "ignores arrays"}: reasonImpliedKind,
	{
		"properties.json",
		"object properties validation",
		"ignores other non-objects",
	}: reasonImpliedKind,
	{
		"properties.json",
		"properties whose names are Javascript object property names",
		"ignores arrays",
	}: reasonImpliedKind,
	{
		"properties.json",
		"properties whose names are Javascript object property names",
		"ignores other non-objects",
	}: reasonImpliedKind,
	{"ref.json", "root pointer ref", "match"}:           reasonImpliedKind,
	{"ref.json", "root pointer ref", "recursive match"}: reasonImpliedKind,
	{
		"ref.json",
		"ref applies alongside sibling keywords",
		"ref valid, maxItems invalid",
	}: reasonUnenforced,
	{"required.json", "required validation", "ignores arrays"}:            reasonImpliedKind,
	{"required.json", "required validation", "ignores strings"}:           reasonImpliedKind,
	{"required.json", "required validation", "ignores other non-objects"}: reasonImpliedKind,
	{"required.json", "required validation", "ignores null"}:              reasonImpliedKind,
	{"required.json", "required validation", "ignores boolean"}:           reasonImpliedKind,
	{
		"uniqueItems.json",
		"uniqueItems validation",
		"non-unique array of integers is invalid",
	}: reasonUnenforced,
	{
		"uniqueItems.json",
		"uniqueItems validation",
		"non-unique array of more than two integers is invalid",
	}: reasonUnenforced,
	{
		"uniqueItems.json",
		"uniqueItems validation",
		"numbers are unique if mathematically unequal",
	}: reasonUnenforced,
	{
		"uniqueItems.json",
		"uniqueItems validation",
		"non-unique array of strings is invalid",
	}: reasonUnenforced,
	{
		"uniqueItems.json",
		"uniqueItems validation",
		"non-unique array of objects is invalid",
	}: reasonUnenforced,
	{
		"uniqueItems.json",
		"uniqueItems validation",
		"property order of array of objects is ignored",
	}: reasonUnenforced,
	{
		"uniqueItems.json",
		"uniqueItems validation",
		"non-unique array of nested objects is invalid",
	}: reasonUnenforced,
	{
		"uniqueItems.json",
		"uniqueItems validation",
		"non-unique array of arrays is invalid",
	}: reasonUnenforced,
	{
		"uniqueItems.json",
		"uniqueItems validation",
		"non-unique array of more than two arrays is invalid",
	}: reasonUnenforced,
	{
		"uniqueItems.json",
		"uniqueItems validation",
		"non-unique heterogeneous types are invalid",
	}: reasonUnenforced,
	{
		"uniqueItems.json",
		"uniqueItems validation",
		"objects are non-unique despite key order",
	}: reasonUnenforced,
}

// suiteGenerated is the least number of groups of each top-level file of
// the suite that Generate accepts. A file generating fewer means Generate
// rejects schemas it used to accept. A file missing here generates none.
var suiteGenerated = map[string]int{
	"additionalProperties.json":    3,
	"allOf.json":                   2,
	"anchor.json":                  1,
	"anyOf.json":                   5,
	"boolean_schema.json":          1,
	"const.json":                   10,
	"content.json":                 4,
	"default.json":                 3,
	"dependentRequired.json":       4,
	"dynamicRef.json":              1,
	"enum.json":                    8,
	"exclusiveMaximum.json":        1,
	"exclusiveMinimum.json":        1,
	"format.json":                  19,
	"infinite-loop-detection.json": 1,
	"items.json":                   3,
	"maxContains.json":             1,
	"maxItems.json":                2,
	"maxLength.json":               2,
	"maxProperties.json":           3,
	"maximum.json":                 2,
	"minContains.json":             1,
	"minItems.json":                2,
	"minLength.json":               2,
	"minProperties.json":           2,
	"minimum.json":                 2,
	"multipleOf.json":              6,
	"oneOf.json":                   5,
	"pattern.json":                 3,
	"properties.json":              2,
	"ref.json":                     15,
	"required.json":                3,
	"type.json":                    8,
	"uniqueItems.json":             2,
	"vocabulary.json":              1,
}

// TestSuiteBehavior generates a package from every group of the top-level
// files of the draft 2020-12 suite that Generate accepts, builds them with
// a driver into one program and runs it. The driver decodes each instance
// of a group into the root type of its package. A decode succeeds exactly
// when the suite calls the instance valid, except for the tests of
// suiteAllowed, which must disagree. Each file generates at least the
// groups suiteGenerated records.
//
// The driver encodes every value that decodes, decodes the result and
// encodes it again. The second decode must succeed and both encodings must
// be equal. They are not compared with the instance, which decoding may
// change by design: it drops unknown members, an anyOf keeps the first
// member that matches, and 1.0 encodes as 1.
func TestSuiteBehavior(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a program from the generated packages")
	}

	root := filepath.FromSlash("../../pkg/jsonschema/testdata/suite/tests/draft2020-12")

	files, err := filepath.Glob(filepath.Join(root, "*.json"))
	assert.NilError(t, err)

	mod := suiteModule(t)

	var (
		keys    []suiteKey
		valid   []bool
		schemas []jsontext.Value
		driver  suiteDriver
		groups  int
		skipped int
	)

	generated := map[string]int{}

	for _, path := range files {
		b, err := os.ReadFile(path)
		assert.NilError(t, err)

		var gs []suiteGroup
		assert.NilError(t, jsonv2.Unmarshal(b, &gs))

		file := filepath.Base(path)

		for gi, g := range gs {
			pkg := suitePackage(file, gi)

			src, err := generateSuitePackage(g.Schema, pkg)
			if errors.Is(err, ErrUnsupported) || errors.Is(err, ErrConflict) {
				skipped++
				continue
			}

			assert.NilError(t, err, "%s: %s", file, g.Description)

			groups++
			generated[file]++

			writeFile(t, filepath.Join(mod, pkg, pkg+".kama_jsonschema.go"), src)

			for _, c := range g.Tests {
				keys = append(keys, suiteKey{file: file, group: g.Description, test: c.Description})
				valid = append(valid, c.Valid)
				schemas = append(schemas, g.Schema)

				driver.add(pkg, c.Data)
			}
		}
	}

	checkGenerated(t, generated)

	writeFile(t, filepath.Join(mod, "main.go"), driver.source())

	results := runSuiteDriver(t, mod)
	assert.Equal(t, len(results), len(keys))

	agree, allowed := 0, map[string]int{}

	for i, k := range keys {
		if msg, ok := strings.CutPrefix(results[i], "E "); ok {
			t.Errorf("%s: %s: %s: round trip: %s", k.file, k.group, k.test, msg)
		}

		got := results[i] != "0"
		reason, ok := suiteAllowed[k]

		switch {
		case got == valid[i] && ok:
			t.Errorf("%s: %s: %s: agrees but is allowed to disagree: %s",
				k.file, k.group, k.test, reason)
		case got == valid[i]:
			agree++
		case ok:
			allowed[reason]++

			if err := checkAllowed(reason, schemas[i], valid[i]); err != nil {
				t.Errorf("%s: %s: %s: %v", k.file, k.group, k.test, err)
			}
		default:
			t.Errorf("%s: %s: %s: valid is %t, decoding succeeded is %t",
				k.file, k.group, k.test, valid[i], got)
		}
	}

	assert.Equal(t, len(slices.Compact(slices.SortedFunc(slices.Values(keys), compareSuiteKeys))),
		len(keys), "a test of the suite is located twice")

	for k := range suiteAllowed {
		if !slices.Contains(keys, k) {
			t.Errorf("%s: %s: %s: allowed but not checked", k.file, k.group, k.test)
		}
	}

	t.Logf("%d groups generated, %d unsupported; %d instances checked, %d agree",
		groups, skipped, len(keys), agree)

	for _, reason := range slices.Sorted(maps.Keys(allowed)) {
		t.Logf("allowed: %d: %s", allowed[reason], reason)
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

func compareSuiteKeys(a, b suiteKey) int {
	return cmp.Or(cmp.Compare(a.file, b.file), cmp.Compare(a.group, b.group),
		cmp.Compare(a.test, b.test))
}

// checkAllowed reports a test that does not fit the reason it is allowed
// to disagree for.
func checkAllowed(reason string, schema jsontext.Value, valid bool) error {
	switch reason {
	case reasonUnenforced:
		held, err := holdsKeyword(schema, unenforcedKeywords)
		if err != nil {
			return err
		}

		if !held {
			return errors.New("the schema holds no pure validation keyword")
		}
	case reasonImpliedKind:
		if !valid {
			return errors.New("the suite calls the instance invalid")
		}
	default:
		return fmt.Errorf("unknown reason %q", reason)
	}

	return nil
}

// holdsKeyword reports whether a schema of raw that the generator reads
// holds one of keywords as a keyword. A property of that name is no
// keyword.
func holdsKeyword(raw jsontext.Value, keywords []string) (bool, error) {
	var doc jsonschema.Schema
	if err := jsonv2.Unmarshal(raw, &doc); err != nil {
		return false, err
	}

	for _, s := range schemas(&doc) {
		b, err := jsonv2.Marshal(s)
		if err != nil {
			return false, err
		}

		var members map[string]jsontext.Value

		if jsontext.Value(b).Kind() != '{' {
			continue
		}

		if err := jsonv2.Unmarshal(b, &members); err != nil {
			return false, err
		}

		if slices.ContainsFunc(keywords, func(kw string) bool { _, ok := members[kw]; return ok }) {
			return true, nil
		}
	}

	return false, nil
}

// suitePackage names the package of group gi of file after both, so a
// failing build points to the group.
func suitePackage(file string, gi int) string {
	name := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}

		return '_'
	}, strings.TrimSuffix(file, ".json"))

	return name + "_" + strconv.Itoa(gi)
}

// generateSuitePackage returns the file Generate gives for raw as the
// package pkg with the root type Root.
func generateSuitePackage(raw jsontext.Value, pkg string) ([]byte, error) {
	var doc jsonschema.Schema
	if err := jsonv2.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}

	files, _, err := Generate(&doc, Options{
		Package:  pkg,
		TypeName: "Root",
		Schema:   "schema.json",
		Now:      gentest.Now,
	})
	if err != nil {
		return nil, err
	}

	return files[0].Content, nil
}

// suiteDriver collects the decodes the driver program runs.
type suiteDriver struct {
	pkgs  []string
	cases []string
}

func (d *suiteDriver) add(pkg string, data jsontext.Value) {
	if !slices.Contains(d.pkgs, pkg) {
		d.pkgs = append(d.pkgs, pkg)
	}

	d.cases = append(
		d.cases,
		fmt.Sprintf("\t{check[%s.Root], %s},\n", pkg, strconv.Quote(string(data))),
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

	for _, pkg := range d.pkgs {
		fmt.Fprintf(&b, "\t%q\n", suiteModulePath+"/"+pkg)
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

// driverCheck is the function of the driver deciding the line of a case.
// Deterministic sorts the members of a map, which would otherwise make two
// encodings of one value differ.
const driverCheck = `func check[T any](b []byte) string {
	var v T
	if json.Unmarshal(b, &v) != nil {
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

// suiteModule creates a module that resolves this module from its
// directory and returns its root.
func suiteModule(t *testing.T) string {
	t.Helper()

	kamalib, err := filepath.Abs("../..")
	assert.NilError(t, err)

	gomod, err := os.ReadFile(filepath.Join(kamalib, "go.mod"))
	assert.NilError(t, err)

	// A go line below that of this module would need go mod tidy.
	goLine := opiter.WrapSeq(strings.Lines(string(gomod))).
		Filter(func(l string) bool { return strings.HasPrefix(l, "go ") }).
		First()
	assert.Assert(t, goLine.IsSome())

	// The sums of this module cover every module the generated code
	// reaches through it.
	gosum, err := os.ReadFile(filepath.Join(kamalib, "go.sum"))
	assert.NilError(t, err)

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), []byte("module "+suiteModulePath+"\n\n"+
		goLine.Value()+"\n"+
		"require github.com/ngicks/kama/kamalib v0.0.0\n\n"+
		"replace github.com/ngicks/kama/kamalib => "+kamalib+"\n"))
	writeFile(t, filepath.Join(root, "go.sum"), gosum)

	return root
}

// runSuiteDriver runs the driver of the module at root and returns its
// line per case.
func runSuiteDriver(t *testing.T, root string) []string {
	t.Helper()

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = root

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
