package dircmnt

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ngicks/opiter"
)

// KeyValue is one arg of a directive comment, written key, key=value or
// key(arg, ...).
type KeyValue struct {
	Key   string
	Value string
	// HasValue is true for a pair written key=value or key=. It tells key
	// from key=, which both leave Value empty. A comparison has no value,
	// even when it holds "=", as in ==3.
	HasValue bool
	// KeyQuoted and ValueQuoted tell that Key and Value were written as
	// quoted strings. They let a reader tell the string "1" from the
	// number 1.
	KeyQuoted, ValueQuoted bool
	// Call is true for a call written key(arg, ...), as in or(a, b=1). Key
	// is its name and Args holds its args in order. key() is a call without
	// args. A call has no value.
	Call bool
	Args []KeyValue
}

// String spells kv as [ParseArgsKeyValue] reads it back: key, key=value or
// key(arg, ...), with a token quoted when it was written quoted or when a
// bare token cannot hold it. A token holding a double quote or a backslash
// is quoted as a raw string literal when one can hold it. A comparison is spelled as written;
// quoting it
// would turn it into a key.
func (kv KeyValue) String() string { return kv.spell(topLevel) }

// Arg returns the text kv passes on to what its key names: the value of a
// pair, or the args of a call as [SpellCallArgs] spells them.
func (kv KeyValue) Arg() string {
	if kv.Call {
		return SpellCallArgs(kv.Args)
	}

	return kv.Value
}

// ParseArgsKeyValue parses [Comment.Args] as space-separated key=value
// pairs and calls.
//
// The grammar is:
//
//	args       = { space } [ arg { space { space } arg } ] { space } .
//	arg        = comparison | call | pair .
//	comparison = op { op } [ quoted | bare ] .
//	op         = "<" | ">" | "=" | "!" .
//	call       = name "(" { space } [ arg { { space } "," { space } arg } { space } ] ")" .
//	name       = bare .
//	pair       = key [ "=" value ] .
//	key        = quoted | bare .
//	value      = quoted | bare .
//	quoted     = Go interpreted or raw string literal, unquoted with strconv.Unquote .
//	bare       = run of characters other than space, `"` and "`" .
//
// space is a character for which unicode.IsSpace reports true. An arg
// starting with an op is always a comparison, so a bare key never starts
// with "<", ">", "=" or "!". A bare key is not empty and ends at the first
// "=" or "("; a bare value may be empty and may hold "=". A key is not
// empty, quoted or not.
//
// A bare key directly followed by "(" is the name of a call, whose args
// follow the same grammar and are separated by commas, as in
// or(cmp=">=1", not(required)). Inside the parentheses a bare token also
// ends at ",", "(" and ")", so a token holding one of them is quoted there.
// Spaces around the args are ignored. A call is followed by a space or the
// end of the args, or by "," or ")" inside another call. Outside a call
// "," and ")" are ordinary characters, and a value or a comparison may hold
// "(", as in regexp=^(a|b)$.
//
// A comparison, such as >=1, ==3 or !="a b", is a key without a value. Its
// key is the comparison as written: the "=" it holds does not start a
// value, and a quoted literal in it is kept with its quotes, so that the
// reader can tell the literal "1" from the number 1.
//
// The args are returned in the order they are written. A key given twice
// is returned twice.
func ParseArgsKeyValue(args string) ([]KeyValue, error) {
	var out []KeyValue

	rest := args
	for {
		rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
		if rest == "" {
			return out, nil
		}

		kv, r, err := parseArg(rest, topLevel)
		if err != nil {
			return nil, err
		}

		out = append(out, kv)
		rest = r
	}
}

// ParseCallArgs parses args, the text between the parentheses of a call,
// as comma-separated args in the grammar of [ParseArgsKeyValue]. It reads
// back what [SpellCallArgs] spells. Empty args hold no arg.
func ParseCallArgs(args string) ([]KeyValue, error) {
	out, _, err := parseCallArgs(args, "")
	return out, err
}

// SpellArgs spells kv as space-separated args, in the grammar
// [ParseArgsKeyValue] reads back.
func SpellArgs(kv []KeyValue) string {
	return strings.Join(
		opiter.WrapSeq(slices.Values(kv)).
			Map(KeyValue.String).
			CollectSlice(),
		" ",
	)
}

// SpellCallArgs spells kv as the args of a call, separated by ", ", in the
// grammar [ParseCallArgs] reads back.
func SpellCallArgs(kv []KeyValue) string {
	return strings.Join(
		opiter.WrapSeq(slices.Values(kv)).
			Map(func(kv KeyValue) string { return kv.spell(inCall) }).
			CollectSlice(),
		", ",
	)
}

// BareWords returns the keys of kv, or false when one of them has a value
// or is a call.
func BareWords(kv []KeyValue) ([]string, bool) {
	notBare := opiter.WrapSeq(slices.Values(kv)).
		Any(func(kv KeyValue) bool { return kv.HasValue || kv.Call })

	if notBare {
		return nil, false
	}

	return opiter.WrapSeq(slices.Values(kv)).
		Map(func(kv KeyValue) string { return kv.Key }).
		CollectSlice(), true
}

// KeyValueMap returns kv keyed by name. A pair without "=" maps its name to
// "". A name given twice and a call are errors.
func KeyValueMap(kv []KeyValue) (map[string]string, error) {
	out := make(map[string]string, len(kv))
	for _, p := range kv {
		if p.Call {
			return nil, fmt.Errorf("call %s; want name or name=value", p.String())
		}

		if _, ok := out[p.Key]; ok {
			return nil, fmt.Errorf("duplicate name %q", p.Key)
		}

		out[p.Key] = p.Value
	}

	return out, nil
}

// place is where an arg is written: among the args of a directive or
// between the parentheses of a call.
type place bool

const (
	topLevel place = false
	inCall   place = true
)

// breaks returns the characters that end a bare token at p besides a
// space.
func (p place) breaks() string {
	if p == inCall {
		return ",()"
	}

	return ""
}

// keyBreaks returns the characters that end a bare key at p besides a
// space.
func (p place) keyBreaks() string { return "=(" + p.breaks() }

// parseArg parses the arg at the start of s, which is not empty and does
// not start with a space, and returns the text after it.
func parseArg(s string, p place) (KeyValue, string, error) {
	var kv KeyValue

	if strings.ContainsRune(comparisonOps, rune(s[0])) {
		key, rest, err := cutComparison(s, p.breaks())
		if err != nil {
			return kv, "", fmt.Errorf("dircmnt: comparison at %q: %w", s, err)
		}

		kv.Key = key

		return kv, rest, nil
	}

	kv.KeyQuoted = startsQuoted(s)

	key, rest, err := cutToken(s, p.keyBreaks())
	if err != nil {
		return kv, "", fmt.Errorf("dircmnt: key at %q: %w", s, err)
	}

	if key == "" {
		return kv, "", fmt.Errorf("dircmnt: missing key at %q", s)
	}

	kv.Key = key

	switch {
	case strings.HasPrefix(rest, "("):
		if kv.KeyQuoted {
			return kv, "", fmt.Errorf("dircmnt: quoted name of a call at %q", s)
		}

		kv.Call = true

		kv.Args, rest, err = parseCallArgs(rest[1:], key)
		if err != nil {
			return kv, "", err
		}

		if r, _ := utf8.DecodeRuneInString(
			rest,
		); p == topLevel && rest != "" &&
			!unicode.IsSpace(r) {
			return kv, "", fmt.Errorf("dircmnt: text after the call %s at %q", key, rest)
		}
	case strings.HasPrefix(rest, "="):
		kv.HasValue = true
		kv.ValueQuoted = startsQuoted(rest[1:])

		kv.Value, rest, err = cutToken(rest[1:], p.breaks())
		if err != nil {
			return kv, "", fmt.Errorf("dircmnt: value of %q: %w", kv.Key, err)
		}
	}

	return kv, rest, nil
}

// parseCallArgs parses comma-separated args from the start of s. For the
// call named name it parses up to the closing parenthesis and returns the
// text after it. With an empty name it parses all of s.
func parseCallArgs(s, name string) ([]KeyValue, string, error) {
	closed := name != ""

	var out []KeyValue

	rest := strings.TrimLeftFunc(s, unicode.IsSpace)

	switch {
	case closed && strings.HasPrefix(rest, ")"):
		return nil, rest[1:], nil
	case !closed && rest == "":
		return nil, "", nil
	}

	for {
		if rest == "" {
			return nil, "", fmt.Errorf("dircmnt: missing ) of the call %s", name)
		}

		kv, r, err := parseArg(rest, inCall)
		if err != nil {
			return nil, "", err
		}

		out = append(out, kv)
		rest = strings.TrimLeftFunc(r, unicode.IsSpace)

		switch {
		case strings.HasPrefix(rest, ","):
			rest = strings.TrimLeftFunc(rest[1:], unicode.IsSpace)
			if rest == "" || strings.HasPrefix(rest, ")") {
				return nil, "", fmt.Errorf(
					"dircmnt: missing arg after , following %s",
					kv.spell(inCall),
				)
			}
		case closed && strings.HasPrefix(rest, ")"):
			return out, rest[1:], nil
		case !closed && rest == "":
			return out, "", nil
		case closed && rest == "":
			return nil, "", fmt.Errorf("dircmnt: missing ) of the call %s", name)
		case closed:
			return nil, "", fmt.Errorf(
				"dircmnt: want , or ) after %s, got %q", kv.spell(inCall), rest,
			)
		default:
			return nil, "", fmt.Errorf("dircmnt: want , after %s, got %q", kv.spell(inCall), rest)
		}
	}
}

// spell spells kv as written at p.
func (kv KeyValue) spell(p place) string {
	switch {
	case kv.Call:
		return kv.Key + "(" + SpellCallArgs(kv.Args) + ")"
	case kv.HasValue:
		return spellToken(kv.Key, kv.KeyQuoted, p.keyBreaks()) + "=" +
			spellToken(kv.Value, kv.ValueQuoted, p.breaks())
	case kv.isComparison():
		return kv.Key
	}

	return spellToken(kv.Key, kv.KeyQuoted, p.keyBreaks())
}

// isComparison reports whether kv is a comparison, as in >=1.
func (kv KeyValue) isComparison() bool {
	return !kv.HasValue && !kv.Call && !kv.KeyQuoted && kv.Key != "" &&
		strings.ContainsAny(kv.Key[:1], comparisonOps)
}

// spellToken quotes s when it was quoted or a bare token cannot hold it: s
// holds a space, a quote or one of breaks. An empty bare token stays bare,
// so that key= spells back as key=.
func spellToken(s string, quoted bool, breaks string) string {
	if quoted || strings.ContainsFunc(s, isBareBreak) || strings.ContainsAny(s, breaks) {
		return quoteToken(s)
	}

	return s
}

// quoteToken quotes s as a raw string literal when s holds a double quote
// or a backslash, which an interpreted literal escapes, and a raw literal
// holds every character of s on one line. Otherwise it quotes s as an
// interpreted literal. Quoting a quoted token again then adds no escapes.
func quoteToken(s string) string {
	if strings.ContainsAny(s, `"\`) && !strings.Contains(s, "`") &&
		!strings.ContainsFunc(s, func(r rune) bool { return !unicode.IsPrint(r) }) {
		return "`" + s + "`"
	}

	return strconv.Quote(s)
}

// isBareBreak reports whether a bare token cannot hold r anywhere.
func isBareBreak(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune(quotes, r) }

const quotes = "\"`"

// startsQuoted reports whether s starts with a quoted token.
func startsQuoted(s string) bool { return s != "" && strings.IndexByte(quotes, s[0]) >= 0 }

// comparisonOps are the characters a comparison starts with.
const comparisonOps = "<>=!"

// cutComparison cuts the comparison at the start of s, as written, and
// returns the text after it. Its operand ends as a value does, at a space
// or at any of terminators.
func cutComparison(s, terminators string) (token, rest string, err error) {
	ops := len(s) - len(strings.TrimLeft(s, comparisonOps))

	// cutToken unquotes a quoted operand; the comparison keeps it as written,
	// so only the rest is used.
	_, rest, err = cutToken(s[ops:], terminators)
	if err != nil {
		return "", "", err
	}

	return s[:len(s)-len(rest)], rest, nil
}

// cutToken cuts the quoted or bare token at the start of s and returns the
// text after it. A bare token ends at a space or at any of terminators,
// and a quoted token must be followed by one of them or by the end of s.
func cutToken(s, terminators string) (token, rest string, err error) {
	ends := func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune(terminators, r) }

	if startsQuoted(s) {
		token, rest, err = cutQuoted(s)
		if err != nil {
			return "", "", err
		}

		if r, _ := utf8.DecodeRuneInString(rest); rest != "" && !ends(r) {
			return "", "", errors.New("text after closing quote")
		}

		return token, rest, nil
	}

	end := strings.IndexFunc(s, ends)
	if end < 0 {
		end = len(s)
	}

	token, rest = s[:end], s[end:]
	if strings.ContainsAny(token, quotes) {
		return "", "", errors.New("quote inside unquoted text")
	}

	return token, rest, nil
}

// cutQuoted unquotes the Go string literal at the start of s and returns
// the text after it.
func cutQuoted(s string) (value, rest string, err error) {
	// strconv.QuotedPrefix reports an unterminated literal and an invalid
	// escape with the same error, so the closing quote is searched here.
	for i := 1; i < len(s); i++ {
		switch {
		case s[i] == '\\' && s[0] == '"':
			i++
		case s[i] == s[0]:
			value, err = strconv.Unquote(s[:i+1])
			return value, s[i+1:], err
		}
	}

	return "", "", errors.New("unterminated quoted string")
}
