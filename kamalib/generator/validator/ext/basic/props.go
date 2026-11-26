package basic

import (
	"errors"
	"fmt"
	"go/types"
	"regexp"
	"slices"
	"strconv"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// Props is the validator //kama:validate props(<entry>, ...): each member
// of an object passes the validators of the entries applying to it, as
// JSON Schema properties, patternProperties and additionalProperties test.
// An entry is one of these:
//
//	field("<name>", <validator>, ...)    the member name, when present
//	match("<pattern>", <validator>, ...) each member whose name the RE2 pattern matches
//	rest(<validator>, ...)               each member no field names and no match matches
//
// An entry without validators applies and always holds, which tells rest
// the member is no other one.
//
// It applies to raw JSON text only, as IsRawJson tells, and passes a value
// of another kind. It returns the errors of the validators, each located
// at its member. As an Annotator it records each member an entry applies
// to.
type Props struct{}

// Name returns "props".
func (Props) Name() string { return "props" }

// PropsArgs are the args of props(<entry>, ...), which Parse reads and
// Call spells. Each entry is the call FieldArgs, MatchArgs or RestArgs
// spells.
type PropsArgs struct {
	Entries []expr.Node `json:"entries"`
}

// Call returns the call of props spelling a.
func (a PropsArgs) Call() (expr.Call, error) { return propsParams.Call(Props{}.Name(), a) }

// FieldArgs are the args of the entry field("<name>", <validator>, ...)
// of props. Name is a pointer since "" names a member too.
type FieldArgs struct {
	Name     *string     `json:"name"`
	Operands []expr.Node `json:"operands"`
}

// Call returns the entry field spelling a.
func (a FieldArgs) Call() (expr.Call, error) { return namedParams.Call(fieldEntry, a) }

// MatchArgs are the args of the entry match("<pattern>", <validator>, ...)
// of props. Name holds the pattern.
type MatchArgs FieldArgs

// Call returns the entry match spelling a.
func (a MatchArgs) Call() (expr.Call, error) { return namedParams.Call(matchEntry, a) }

// RestArgs are the args of the entry rest(<validator>, ...) of props and
// of the last arg of prefix.
type RestArgs OperandArgs

// Call returns rest spelling a.
func (a RestArgs) Call() (expr.Call, error) { return operandArgs.Call(restEntry, a) }

// The names of the entries of props. rest is the last arg of prefix too.
const (
	fieldEntry = "field"
	matchEntry = "match"
	restEntry  = "rest"
)

var (
	propsParams = ext.Params{Variadic: "entries", Rules: []string{"entries"}}
	namedParams = ext.Params{
		Positional: []string{"name"},
		Variadic:   operandArgs.Variadic,
		Rules:      operandArgs.Rules,
	}
)

// PropsOptions are the options of //kama:validate props(<entry>, ...).
type PropsOptions struct {
	// Entries are the entries of the args, in order.
	Entries []PropsEntry
}

// PropsEntry is an entry of props.
type PropsEntry struct {
	// Kind is field, match or rest.
	Kind string
	// Name is the member name of field and the pattern of match.
	Name string
	// Operands are the validators of the entry.
	Operands []Operand
}

// Parse decodes the args of c, calls of field, match and rest, into
// entries, resolving their operands through r.
func (Props) Parse(c expr.Call, r ext.Resolver) (ext.Rule, error) {
	args, err := propsParams.Bind(r, c, nil)
	if err != nil {
		return nil, err
	}

	var (
		o    PropsOptions
		seen []string
	)

	for _, a := range args {
		entry, err := propsEntry(a.Value, r)
		if err != nil {
			return nil, err
		}

		key := entry.Kind + " " + entry.Name
		if slices.Contains(seen, key) {
			return nil, argsError(fmt.Errorf("entry %s %q given twice", entry.Kind, entry.Name))
		}

		seen = append(seen, key)
		o.Entries = append(o.Entries, entry)
	}

	return o, nil
}

func propsEntry(n expr.Node, r ext.Resolver) (PropsEntry, error) {
	c, ok := n.(expr.Call)
	if !ok {
		return PropsEntry{}, argsError(fmt.Errorf("entry %s is no call of field, match or rest", n))
	}

	switch c.Name {
	case fieldEntry, matchEntry:
		name, ops, err := parseNamed(c, r)
		if err != nil {
			return PropsEntry{}, err
		}

		if c.Name == matchEntry {
			if _, err := regexp.Compile(name); err != nil {
				return PropsEntry{}, argsError(
					fmt.Errorf("pattern %q does not compile: %w", name, err),
				)
			}
		}

		return PropsEntry{Kind: c.Name, Name: name, Operands: ops}, nil
	case restEntry:
		ops, err := parseOperandArgs(r, c)
		if err != nil {
			return PropsEntry{}, fmt.Errorf("rest: %w", err)
		}

		return PropsEntry{Kind: c.Name, Operands: ops}, nil
	}

	return PropsEntry{}, argsError(
		fmt.Errorf("entry %s is no call of field, match or rest", c.Name),
	)
}

// Check accepts raw JSON text whose members every validator applies to.
func (o PropsOptions) Check(t types.Type) error {
	errs := opiter.SlicesValues(o.Entries).
		Map(func(en PropsEntry) error { return checkRaw("props", en.Operands, t) }).
		CollectSlice()

	return errors.Join(errs...)
}

// Emit calls rawjson.Properties recording nothing.
func (o PropsOptions) Emit(e ext.Emitter, t types.Type, v ext.Gen) (ext.Gen, error) {
	return o.Annotate(e, t, v, noRecord)
}

// Annotate calls rawjson.Properties with a function literal per entry.
func (o PropsOptions) Annotate(e ext.Emitter, t types.Type, v, ev ext.Gen) (ext.Gen, error) {
	args := []ext.Gen{e.StopOnFirstError(), ev}

	for _, en := range o.Entries {
		f, err := valueFunc(e, en.Operands, t)
		if err != nil {
			return nil, fmt.Errorf("%s %q: %w", en.Kind, en.Name, err)
		}

		var g ext.Gen

		switch en.Kind {
		case fieldEntry:
			g = lang.Expr(
				"%s.Property(%s, %s)",
				e.Import(ext.RawJsonPath),
				strconv.Quote(en.Name),
				f,
			)
		case matchEntry:
			g = lang.Expr("%s.PatternProperty(%s, %s)",
				e.Import(ext.RawJsonPath), declareRegexp(e, en.Name), f)
		default:
			g = lang.Expr("%s.Additional(%s)", e.Import(ext.RawJsonPath), f)
		}

		args = append(args, g)
	}

	return rawCall(e, "Properties", v, args...), nil
}

// declareRegexp declares the package-level variable holding pattern
// compiled and returns its name.
func declareRegexp(e ext.Emitter, pattern string) string {
	re := regexpVarName(pattern)
	e.Declare(re, func(imp ext.Importer) ext.Step {
		return lang.Stmt("var %s = %s.MustCompile(%s)",
			re, imp.Import("regexp"), strconv.Quote(pattern))
	})

	return re
}
