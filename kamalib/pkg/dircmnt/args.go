package dircmnt

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// KeyValue is one arg of a directive comment, written key or key=value.
type KeyValue struct {
	Key   string
	Value string
	// HasValue is false for an arg without "=". It tells key from key=,
	// which both leave Value empty.
	HasValue bool
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
// space is a character for which unicode.IsSpace reports true. A bare key
// is not empty and ends at the first "="; a bare value may be empty and may
// hold "=". A key is not empty, quoted or not.
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

		kv.Key, rest, err = cutToken(rest, "=")
		if err != nil {
			return nil, fmt.Errorf("dircmnt: key at %q: %w", at, err)
		}

		if kv.Key == "" {
			return nil, fmt.Errorf("dircmnt: missing key at %q", at)
		}

		if strings.HasPrefix(rest, "=") {
			kv.HasValue = true

			kv.Value, rest, err = cutToken(rest[1:], "")
			if err != nil {
				return nil, fmt.Errorf("dircmnt: value of %q: %w", kv.Key, err)
			}
		}

		out = append(out, kv)
	}
}

const quotes = "\"`"

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

	if s != "" && strings.IndexByte(quotes, s[0]) >= 0 {
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
