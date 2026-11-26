package ext

import (
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"go/types"
	"slices"
	"strconv"

	"github.com/ngicks/kama/kamalib/generator/genext"
	"github.com/ngicks/kama/kamalib/pkg/unknownkey"
)

// TextSectionName is the key of the section of the validator in the
// description of a type in a text extension file.
const TextSectionName = "validate"

// TextHelperPrefix starts the names of the helpers of text extension files
// the validator declares, as kamaValidateExtTrim for a helper trim.
const TextHelperPrefix = "kamaValidateExt"

func init() { genext.RegisterTextSection(TextSectionName, buildTextSection) }

// TextSection is the section of the validator in the description of a type
// in a text extension file. Each part it holds makes the support answer
// one optional interface of this package.
type TextSection struct {
	// Present answers PresenceTester.
	Present *TextCond `json:"present"`
	// Zero answers ZeroTester.
	Zero *TextCond `json:"zero"`
	// Null answers NullTester. Its words are not used.
	Null *TextCond `json:"null"`
	// Length answers LengthTester.
	Length *TextLength `json:"length"`
	// Compare answers Comparer.
	Compare *TextCompare   `json:"compare"`
	Unknown map[string]any `json:",embed"`
}

// TextCond is a condition on a value.
type TextCond struct {
	// Cond is the template of the condition, with the value as {{.V}}.
	Cond string `json:"cond"`
	// Words tell what the value is when the condition does not hold, as
	// "undefined" for present and "a non-zero value" for zero.
	Words   string         `json:"words"`
	Unknown map[string]any `json:",embed"`
}

// TextLength is the number of values a value holds.
type TextLength struct {
	// Expr is the template of the expression of type int, with the value as
	// {{.V}}.
	Expr    string         `json:"expr"`
	Unknown map[string]any `json:",embed"`
}

// TextCompare is how cmp and multipleof compare a value with an operand.
type TextCompare struct {
	// Ops are the operators the type takes among >, >=, <, <=, ==, != and
	// OpMultiple. An operator outside them is left to the rule.
	Ops []string `json:"ops"`
	// Operand is "string" when the operand must be a quoted string and
	// "number" when it must be a number.
	Operand string `json:"operand"`
	// Cond is the template of the condition, with the value as {{.V}}, the
	// operator as {{.Op}} and the operand as {{.Operand}}, the Go literal
	// the directive wrote.
	Cond    string         `json:"cond"`
	Unknown map[string]any `json:",embed"`
}

func (s *TextSection) unknownKeys() [][]string {
	paths := unknownkey.Paths(s.Unknown)

	if s.Present != nil {
		paths = append(paths, unknownkey.Paths(s.Present.Unknown, "present")...)
	}

	if s.Zero != nil {
		paths = append(paths, unknownkey.Paths(s.Zero.Unknown, "zero")...)
	}

	if s.Null != nil {
		paths = append(paths, unknownkey.Paths(s.Null.Unknown, "null")...)
	}

	if s.Length != nil {
		paths = append(paths, unknownkey.Paths(s.Length.Unknown, "length")...)
	}

	if s.Compare != nil {
		paths = append(paths, unknownkey.Paths(s.Compare.Unknown, "compare")...)
	}

	return paths
}

// operators are the operators a TextCompare may name.
var operators = []string{">", ">=", "<", "<=", "==", "!=", OpMultiple}

func buildTextSection(
	scope genext.TextScope,
	raw jsontext.Value,
	path []string,
) (genext.TypeSupport, []error, error) {
	var sec TextSection

	warnings, err := genext.DecodeTextSection(scope, raw, path, &sec, sec.unknownKeys)
	if err != nil {
		return nil, nil, err
	}

	at := func(keys ...string) []string { return append(slices.Clip(path), keys...) }

	var errs []error

	check := func(text string, keys ...string) {
		errs = append(errs, scope.Check(at(keys...), text))
	}

	if sec.Present != nil {
		check(sec.Present.Cond, "present", "cond")
	}

	if sec.Zero != nil {
		check(sec.Zero.Cond, "zero", "cond")
	}

	if sec.Null != nil {
		check(sec.Null.Cond, "null", "cond")
	}

	if sec.Length != nil {
		check(sec.Length.Expr, "length", "expr")
	}

	if c := sec.Compare; c != nil {
		check(c.Cond, "compare", "cond")

		for i, op := range c.Ops {
			if !slices.Contains(operators, op) {
				errs = append(errs, scope.Error(
					at("compare", "ops", strconv.Itoa(i)),
					"%q; want one of %v", op, operators,
				))
			}
		}

		if c.Operand != "string" && c.Operand != "number" {
			errs = append(errs, scope.Error(
				at("compare", "operand"), "%q; want string or number", c.Operand,
			))
		}
	}

	if err := errors.Join(errs...); err != nil {
		return nil, warnings, err
	}

	return textTester{scope: scope, path: path}, warnings, nil
}

// textTester answers the optional interfaces of this package for a type a
// text extension file describes. It decodes its section from the file
// each time it is asked.
type textTester struct {
	scope genext.TextScope
	path  []string
}

var (
	_ PresenceTester = textTester{}
	_ ZeroTester     = textTester{}
	_ NullTester     = textTester{}
	_ LengthTester   = textTester{}
	_ Comparer       = textTester{}
	_ genext.Binder  = textTester{}
)

func (s textTester) Name() string {
	return s.scope.File() + "#" + s.scope.Key() + "#" + TextSectionName
}

func (s textTester) Bind(out genext.Output, helperPrefix string) genext.TypeSupport {
	s.scope = s.scope.Bind(out, helperPrefix)
	return s
}

// section returns the section of s for a type it claims. ok is false for
// another type. The section decoded when the file loaded, so it decodes
// again.
func (s textTester) section(t types.Type) (sec TextSection, ok bool) {
	if !s.scope.Claims(t) {
		return TextSection{}, false
	}

	_ = json.Unmarshal(s.scope.Section(TextSectionName), &sec)

	return sec, true
}

func (s textTester) gen(text string, v Gen, extra map[string]string, keys ...string) Gen {
	return s.scope.Gen(append(slices.Clip(s.path), keys...), text, v, extra)
}

func (s textTester) Presence(t types.Type, v Gen) (Gen, string, bool) {
	sec, ok := s.section(t)
	if !ok || sec.Present == nil {
		return nil, "", false
	}

	return s.gen(sec.Present.Cond, v, nil, "present", "cond"),
		cmp.Or(sec.Present.Words, "absent"), true
}

func (s textTester) Zero(t types.Type, v Gen) (Gen, string, bool) {
	sec, ok := s.section(t)
	if !ok || sec.Zero == nil {
		return nil, "", false
	}

	return s.gen(sec.Zero.Cond, v, nil, "zero", "cond"),
		cmp.Or(sec.Zero.Words, "a non-zero value"), true
}

func (s textTester) Null(t types.Type, v Gen) (Gen, bool) {
	sec, ok := s.section(t)
	if !ok || sec.Null == nil {
		return nil, false
	}

	return s.gen(sec.Null.Cond, v, nil, "null", "cond"), true
}

func (s textTester) Length(t types.Type, v Gen) (Gen, bool) {
	sec, ok := s.section(t)
	if !ok || sec.Length == nil {
		return nil, false
	}

	return s.gen(sec.Length.Expr, v, nil, "length", "expr"), true
}

func (s textTester) Comparable(t types.Type, op, operand string) (bool, error) {
	sec, ok := s.section(t)
	if !ok || sec.Compare == nil || !slices.Contains(sec.Compare.Ops, op) {
		return false, nil
	}

	_, err := strconv.Unquote(operand)
	quoted := err == nil

	switch {
	case sec.Compare.Operand == "string" && !quoted:
		return true, fmt.Errorf("%w: %s compares with a quoted string, got %s", ErrArgs, t, operand)
	case sec.Compare.Operand == "number" && (quoted || operand == "true" || operand == "false"):
		return true, fmt.Errorf("%w: %s compares with a number, got %s", ErrArgs, t, operand)
	}

	return true, nil
}

// Compare writes to e, so that a rule asking an unbound registry still
// imports and declares into the file it emits to.
func (s textTester) Compare(e Emitter, t types.Type, v Gen, op, operand string) Gen {
	sec, _ := s.section(t)
	s.scope = s.scope.Bind(e, TextHelperPrefix)

	return s.gen(sec.Compare.Cond, v, map[string]string{"Op": op, "Operand": operand},
		"compare", "cond")
}
