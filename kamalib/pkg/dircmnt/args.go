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

// KeyValue is one arg of a directive comment, written key or key=value.
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
}

// String spells kv as [ParseArgsKeyValue] reads it back: key or key=value,
// with a token quoted when it was written quoted or when a bare token
// cannot hold it. A comparison is spelled as written; quoting it would turn
// it into a key.
func (kv KeyValue) String() string {
	switch {
	case kv.HasValue:
		return spellToken(kv.Key, kv.KeyQuoted) + "=" + spellToken(kv.Value, kv.ValueQuoted)
	case kv.isComparison():
		return kv.Key
	}

	return spellToken(kv.Key, kv.KeyQuoted)
}

// ParseArgsKeyValue parses [Comment.Args] as space-separated key=value
// pairs.
//
// The grammar is:
//
//	args       = { space } [ arg { space { space } arg } ] { space } .
//	arg        = comparison | pair .
//	comparison = op { op } [ quoted | bare ] .
//	op         = "<" | ">" | "=" | "!" .
//	pair       = key [ "=" value ] .
//	key        = quoted | bare .
//	value      = quoted | bare .
//	quoted     = Go interpreted or raw string literal, unquoted with strconv.Unquote .
//	bare       = run of characters other than space, `"` and "`" .
//
// space is a character for which unicode.IsSpace reports true. An arg
// starting with an op is always a comparison, so a bare key never starts
// with "<", ">", "=" or "!". A bare key is not empty and ends at the first
// "="; a bare value may be empty and may hold "=". A key is not empty,
// quoted or not.
//
// A comparison, such as >=1, ==3 or !="a b", is a key without a value. Its
// key is the comparison as written: the "=" it holds does not start a
// value, and a quoted literal in it is kept with its quotes, so that the
// reader can tell the literal "1" from the number 1.
//
// The pairs are returned in the order they are written. A key given twice
// is returned twice.
func ParseArgsKeyValue(args string) ([]KeyValue, error) {
	var out []KeyValue

	rest := args
	for {
		rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
		if rest == "" {
			return out, nil
		}

		var (
			kv  KeyValue
			err error
		)

		at := rest

		if strings.ContainsRune(comparisonOps, rune(rest[0])) {
			kv.Key, rest, err = cutComparison(rest)
			if err != nil {
				return nil, fmt.Errorf("dircmnt: comparison at %q: %w", at, err)
			}

			out = append(out, kv)

			continue
		}

		kv.KeyQuoted = startsQuoted(rest)

		kv.Key, rest, err = cutToken(rest, "=")
		if err != nil {
			return nil, fmt.Errorf("dircmnt: key at %q: %w", at, err)
		}

		if kv.Key == "" {
			return nil, fmt.Errorf("dircmnt: missing key at %q", at)
		}

		if strings.HasPrefix(rest, "=") {
			kv.HasValue = true
			kv.ValueQuoted = startsQuoted(rest[1:])

			kv.Value, rest, err = cutToken(rest[1:], "")
			if err != nil {
				return nil, fmt.Errorf("dircmnt: value of %q: %w", kv.Key, err)
			}
		}

		out = append(out, kv)
	}
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

// BareWords returns the keys of kv, or false when one of them has a value.
func BareWords(kv []KeyValue) ([]string, bool) {
	hasValue := opiter.WrapSeq(slices.Values(kv)).
		Any(func(kv KeyValue) bool { return kv.HasValue })

	if hasValue {
		return nil, false
	}

	return opiter.WrapSeq(slices.Values(kv)).
		Map(func(kv KeyValue) string { return kv.Key }).
		CollectSlice(), true
}

// KeyValueMap returns kv keyed by name. A pair without "=" maps its name to
// "". A name given twice is an error.
func KeyValueMap(kv []KeyValue) (map[string]string, error) {
	out := make(map[string]string, len(kv))
	for _, p := range kv {
		if _, ok := out[p.Key]; ok {
			return nil, fmt.Errorf("duplicate name %q", p.Key)
		}

		out[p.Key] = p.Value
	}

	return out, nil
}

// isComparison reports whether kv is a comparison, as in >=1.
func (kv KeyValue) isComparison() bool {
	return !kv.HasValue && !kv.KeyQuoted && kv.Key != "" &&
		strings.ContainsAny(kv.Key[:1], comparisonOps)
}

// spellToken quotes s when it was quoted or a bare token cannot hold it. An
// empty bare token stays bare, so that key= spells back as key=.
func spellToken(s string, quoted bool) string {
	if quoted || strings.ContainsFunc(s, isBareBreak) {
		return strconv.Quote(s)
	}

	return s
}

// isBareBreak reports whether a bare token cannot hold r.
func isBareBreak(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune(quotes, r) }

const quotes = "\"`"

// startsQuoted reports whether s starts with a quoted token.
func startsQuoted(s string) bool { return s != "" && strings.IndexByte(quotes, s[0]) >= 0 }

// comparisonOps are the characters a comparison starts with.
const comparisonOps = "<>=!"

// cutComparison cuts the comparison at the start of s, as written, and
// returns the text after it.
func cutComparison(s string) (token, rest string, err error) {
	ops := len(s) - len(strings.TrimLeft(s, comparisonOps))

	// cutToken unquotes a quoted operand; the comparison keeps it as written,
	// so only the rest is used.
	_, rest, err = cutToken(s[ops:], "")
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
