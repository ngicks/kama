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
	// HasValue is true for a pair written key=value or key=. It tells key
	// from key=, which both leave Value empty.
	HasValue bool
	// Quoted is true for a value written as a quoted string. It tells
	// key="1" from key=1, which both leave Value "1".
	Quoted bool
}

// ParseArgsKeyValue parses [Comment.Args] as space-separated key=value
// pairs.
//
// The grammar is:
//
//	args   = { space } [ arg { space { space } arg } ] { space } .
//	arg    = key [ "=" value ] .
//	key    = quoted | bare .
//	value  = quoted | bare .
//	quoted = Go interpreted or raw string literal, unquoted with strconv.Unquote .
//	bare   = run of characters other than space, `"` and "`" .
//
// space is a character for which unicode.IsSpace reports true. A bare key
// is not empty and ends at the first "="; a bare value may be empty and
// may hold "=". A quoted key may be empty, as in "" or ""=1.
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

		kv, r, err := parseArg(rest)
		if err != nil {
			return nil, err
		}

		out = append(out, kv)
		rest = r
	}
}

// parseArg parses the arg at the start of s, which is not empty and does
// not start with a space, and returns the text after it.
func parseArg(s string) (KeyValue, string, error) {
	var kv KeyValue

	key, rest, err := cutToken(s, "=")
	if err != nil {
		return kv, "", fmt.Errorf("dircmnt: key at %q: %w", s, err)
	}

	if key == "" && !startsQuoted(s) {
		return kv, "", fmt.Errorf("dircmnt: missing key at %q", s)
	}

	kv.Key = key

	if strings.HasPrefix(rest, "=") {
		kv.HasValue = true
		kv.Quoted = startsQuoted(rest[1:])

		kv.Value, rest, err = cutToken(rest[1:], "")
		if err != nil {
			return kv, "", fmt.Errorf("dircmnt: value of %q: %w", kv.Key, err)
		}
	}

	return kv, rest, nil
}

const quotes = "\"`"

// startsQuoted reports whether s starts with a quoted token.
func startsQuoted(s string) bool { return s != "" && strings.IndexByte(quotes, s[0]) >= 0 }

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
