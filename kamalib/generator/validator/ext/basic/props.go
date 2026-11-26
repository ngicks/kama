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
	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
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

// Parse decodes arg into entries, parsing their validators through r.
func (Props) Parse(arg string, r ext.Resolver) (ext.Rule, error) {
	args, err := dircmnt.ParseCallArgs(arg)
	if err != nil {
		return nil, argsError(err)
	}

	var (
		o    PropsOptions
		seen []string
	)

	for _, kv := range args {
		entry, err := propsEntry(kv, r)
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

func propsEntry(kv dircmnt.KeyValue, r ext.Resolver) (PropsEntry, error) {
	if !kv.Call {
		return PropsEntry{}, argsError(fmt.Errorf(
			"entry %s is no call of field, match or rest", kv.String(),
		))
	}

	switch kv.Key {
	case "field", "match":
		name, ops, err := parseNamed(kv, r)
		if err != nil {
			return PropsEntry{}, err
		}

		if kv.Key == "match" {
			if _, err := regexp.Compile(name); err != nil {
				return PropsEntry{}, argsError(
					fmt.Errorf("pattern %q does not compile: %w", name, err),
				)
			}
		}

		return PropsEntry{Kind: kv.Key, Name: name, Operands: ops}, nil
	case "rest":
		ops, err := ParseOperands(kv.Arg(), r)
		if err != nil {
			return PropsEntry{}, fmt.Errorf("rest: %w", err)
		}

		return PropsEntry{Kind: kv.Key, Operands: ops}, nil
	}

	return PropsEntry{}, argsError(
		fmt.Errorf("entry %s is no call of field, match or rest", kv.Key),
	)
}

// Check accepts raw JSON text whose members every validator applies to.
func (o PropsOptions) Check(t types.Type) error {
	errs := opiter.WrapSeq(slices.Values(o.Entries)).
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
		case "field":
			g = lang.Expr(
				"%s.Property(%s, %s)",
				e.Import(ext.RawJsonPath),
				strconv.Quote(en.Name),
				f,
			)
		case "match":
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
