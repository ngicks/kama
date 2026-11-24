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
	{"oneOf.json", "oneOf", "first oneOf valid"}:                                reasonUnenforced,
	{"oneOf.json", "oneOf", "neither oneOf valid"}:                              reasonUnenforced,
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

// TestSuiteBehavior generates a package from every group of the top-level
// files of the draft 2020-12 suite that Generate accepts, builds them with
// a driver into one program and runs it. The driver decodes each instance
// of a group into the root type of its package. A decode succeeds exactly
// when the suite calls the instance valid, except for the tests of
// suiteAllowed, which must disagree.
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

			writeFile(t, filepath.Join(mod, pkg, pkg+".kama_jsonschema.go"), src)

			for _, c := range g.Tests {
				keys = append(keys, suiteKey{file: file, group: g.Description, test: c.Description})
				valid = append(valid, c.Valid)
				schemas = append(schemas, g.Schema)

				driver.add(pkg, c.Data)
			}
		}
	}

	writeFile(t, filepath.Join(mod, "main.go"), driver.source())

	got := runSuiteDriver(t, mod)
	assert.Equal(t, len(got), len(keys))

	agree, allowed := 0, map[string]int{}

	for i, k := range keys {
		reason, ok := suiteAllowed[k]

		switch {
		case got[i] == valid[i] && ok:
			t.Errorf("%s: %s: %s: agrees but is allowed to disagree: %s",
				k.file, k.group, k.test, reason)
		case got[i] == valid[i]:
			agree++
		case ok:
			allowed[reason]++

			if err := checkAllowed(reason, schemas[i], valid[i]); err != nil {
				t.Errorf("%s: %s: %s: %v", k.file, k.group, k.test, err)
			}
		default:
			t.Errorf("%s: %s: %s: valid is %t, decoding succeeded is %t",
				k.file, k.group, k.test, valid[i], got[i])
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

func compareSuiteKeys(a, b suiteKey) int {
	return cmp.Or(cmp.Compare(a.file, b.file), cmp.Compare(a.group, b.group),
		cmp.Compare(a.test, b.test))
}

// checkAllowed reports a test that does not fit the reason it is allowed
// to disagree for.
func checkAllowed(reason string, schema jsontext.Value, valid bool) error {
	switch reason {
	case reasonUnenforced:
		mentioned := opiter.WrapSeq(slices.Values(unenforcedKeywords)).
			ContainsFunc(
				func(kw string) bool { return bytes.Contains(schema, []byte(strconv.Quote(kw))) },
			)
		if !mentioned {
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
		fmt.Sprintf("\t{decode[%s.Root], %s},\n", pkg, strconv.Quote(string(data))),
	)
}

// source returns the driver program. It prints 1 for each case whose
// instance decodes and 0 for each that does not, one per line.
func (d *suiteDriver) source() []byte {
	var b bytes.Buffer

	b.WriteString("package main\n\nimport (\n\t\"encoding/json/v2\"\n\t\"fmt\"\n\n")

	for _, pkg := range d.pkgs {
		fmt.Fprintf(&b, "\t%q\n", suiteModulePath+"/"+pkg)
	}

	b.WriteString(")\n\n")
	b.WriteString(
		"func decode[T any](b []byte) error {\n\tvar v T\n\treturn json.Unmarshal(b, &v)\n}\n\n",
	)
	b.WriteString("var cases = []struct {\n\tdecode func([]byte) error\n\tdata   string\n}{\n")

	for _, c := range d.cases {
		b.WriteString(c)
	}

	b.WriteString("}\n\n")
	b.WriteString("func main() {\n\tfor _, c := range cases {\n" +
		"\t\tif c.decode([]byte(c.data)) == nil {\n\t\t\tfmt.Println(1)\n\t\t} else {\n" +
		"\t\t\tfmt.Println(0)\n\t\t}\n\t}\n}\n")

	return b.Bytes()
}

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

// runSuiteDriver runs the driver of the module at root and returns, per
// case, whether its instance decoded.
func runSuiteDriver(t *testing.T, root string) []bool {
	t.Helper()

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = root

	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOPROXY=off")

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	out, err := cmd.Output()
	assert.NilError(t, err, "%s", stderr.String())

	return opiter.WrapSeq(strings.Lines(string(out))).
		Map(func(l string) bool { return strings.TrimSpace(l) == "1" }).
		CollectSlice()
}

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()

	assert.NilError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	assert.NilError(t, os.WriteFile(path, content, 0o644))
}
