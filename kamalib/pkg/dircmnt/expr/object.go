package expr

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// Object binds the args of c to parameters and splits them by kind.
//
// A keyword arg binds to its name. The positional args bind to the names
// of positional in order, and the ones after them to variadic. A
// positional arg beyond positional is an error when variadic is empty.
// A name bound twice is an error.
//
// An arg whose value is a literal, a [String], [Number], [Bool], [Null],
// [List] or [Dict], goes into obj, a JSON object keyed by the bound names,
// which encoding/json/v2 decodes into an options struct. The args bound to
// variadic form one JSON array. A Number keeps every digit as [Json]
// spells it.
//
// An arg whose value is a rule, a [Call], [And], [Or], [Not] or [Cond],
// stays out of obj and is returned in rules, in the order written, with
// Name set to the bound name. A bare identifier, as off in hook=off, is a
// Call without args. Object returns it among the rules, and the caller
// tells a symbol from a rule.
//
// Object does not check the names against the fields of the options
// struct. An unknown name lands in obj like any other.
func Object(
	c Call,
	positional []string,
	variadic string,
) (obj jsontext.Value, rules []Arg, err error) {
	var (
		members  = map[string]any{}
		bound    = map[string]bool{}
		rest     []jsontext.Value
		next     int
		restUsed bool
	)

	for _, a := range c.Args {
		name, isRest := a.Name, false

		switch {
		case name != "":
		case next < len(positional):
			name = positional[next]
			next++
		case variadic != "":
			name, isRest = variadic, true
		default:
			return nil, nil, fmt.Errorf(
				"%w: %s takes %d positional args, got more", ErrArgs, c.Name, len(positional),
			)
		}

		if bound[name] || !isRest && name == variadic && restUsed {
			return nil, nil, fmt.Errorf("%w: %s: %s given twice", ErrArgs, c.Name, name)
		}

		if isRest {
			restUsed = true
		} else {
			bound[name] = true
		}

		if isRule(a.Value) {
			rules = append(rules, Arg{Start: a.Start, Name: name, Value: a.Value})
			continue
		}

		v, err := Json(a.Value)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %s: %w", c.Name, name, err)
		}

		if isRest {
			rest = append(rest, v)
		} else {
			members[name] = v
		}
	}

	if rest != nil {
		members[variadic] = rest
	}

	obj, err = jsonv2.Marshal(members, jsonv2.Deterministic(true))
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %s: %w", ErrArgs, c.Name, err)
	}

	return obj, rules, nil
}

// ErrArgs is wrapped by the errors of [Object] and [Json].
var ErrArgs = errors.New("expr: args do not fit")

// Json returns the JSON text of the literal n. A [Number] becomes a JSON
// number of the same value, without loss: a hexadecimal, octal or binary
// integer is spelled in decimal, and a float is completed with the digits
// JSON requires, as 0.5 for .5 and 1 for 1. . A rule inside a [List] or a
// [Dict], a key of a Dict other than a [String], and a string that is no
// valid UTF-8 are errors wrapping [ErrArgs].
func Json(n Node) (jsontext.Value, error) {
	var b strings.Builder

	enc := jsontext.NewEncoder(&b)
	if err := writeJson(enc, n); err != nil {
		return nil, err
	}

	return jsontext.Value(strings.TrimSuffix(b.String(), "\n")), nil
}

func isRule(n Node) bool {
	switch n.(type) {
	case Call, And, Or, Not, Cond:
		return true
	}

	return false
}

func writeJson(enc *jsontext.Encoder, n Node) error {
	var tok jsontext.Token

	switch x := n.(type) {
	case String:
		tok = jsontext.String(x.Value)
	case Number:
		text, err := jsonNumber(x.Text)
		if err != nil {
			return err
		}

		return wrapArgs(enc.WriteValue(jsontext.Value(text)))
	case Bool:
		tok = jsontext.Bool(x.Value)
	case Null:
		tok = jsontext.Null
	case List:
		return writeList(enc, x)
	case Dict:
		return writeDict(enc, x)
	default:
		return fmt.Errorf("%w: %s is no literal", ErrArgs, n)
	}

	return wrapArgs(enc.WriteToken(tok))
}

func writeList(enc *jsontext.Encoder, x List) error {
	if err := enc.WriteToken(jsontext.BeginArray); err != nil {
		return wrapArgs(err)
	}

	for _, e := range x.Elems {
		if err := writeJson(enc, e); err != nil {
			return err
		}
	}

	return wrapArgs(enc.WriteToken(jsontext.EndArray))
}

func writeDict(enc *jsontext.Encoder, x Dict) error {
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return wrapArgs(err)
	}

	for _, e := range x.Entries {
		k, ok := e.Key.(String)
		if !ok {
			return fmt.Errorf("%w: key %s is no string", ErrArgs, e.Key)
		}

		if err := enc.WriteToken(jsontext.String(k.Value)); err != nil {
			return wrapArgs(err)
		}

		if err := writeJson(enc, e.Value); err != nil {
			return err
		}
	}

	return wrapArgs(enc.WriteToken(jsontext.EndObject))
}

func wrapArgs(err error) error {
	if err != nil {
		return fmt.Errorf("%w: %w", ErrArgs, err)
	}

	return nil
}

// jsonNumber spells the number literal text as a JSON number.
func jsonNumber(text string) (string, error) {
	digits, sign := strings.CutPrefix(text, "-")

	signText := ""
	if sign {
		signText = "-"
	}

	if len(digits) > 1 && digits[0] == '0' && strings.ContainsRune("xXoObB", rune(digits[1])) {
		i, ok := new(big.Int).SetString(digits, 0)
		if !ok {
			return "", fmt.Errorf("%w: number %s", ErrArgs, text)
		}

		return signText + i.String(), nil
	}

	mantissa, exponent := digits, ""
	if i := strings.IndexAny(digits, "eE"); i >= 0 {
		mantissa, exponent = digits[:i], digits[i:]
	}

	whole, frac, _ := strings.Cut(mantissa, ".")

	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}

	out := signText + whole
	if frac != "" {
		out += "." + frac
	}

	out += exponent

	if !jsontext.Value(out).IsValid() {
		return "", fmt.Errorf("%w: number %s", ErrArgs, text)
	}

	return out, nil
}
