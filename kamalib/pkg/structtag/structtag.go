// Package structtag parses and edits Go struct tags.
//
// A tag value is assumed to be formatted like encoding/json's:
//
//	key:"name,opt1,opt2:value"
//
// The name, an option or an option value can be single-quoted to hold
// characters that are otherwise reserved, such as commas:
//
//	json:"'a,b',omitempty"
//
// A trailing comma, as in json:"-," naming a field "-", ends the value with
// an empty option. Get, Add and Delete treat it as no option at all. Add
// drops it when appending an option, so that the empty option never ends up
// before another one. An empty option anywhere else, as in json:"a,,b", is
// a syntax error.
//
// Lifted from github.com/ngicks/go-codegen/graft/pkg/structtag.
package structtag

// Parse is derived from reflect.StructTag.Lookup of the Go standard library.
//
// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found at https://go.dev/LICENSE.

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ngicks/opiter"
)

var (
	// ErrUnpairedKey is returned when a key has no quoted value, as in
	// `json:foo` or `json:"`.
	ErrUnpairedKey = errors.New("structtag: unpaired key")
	// ErrNotFound is returned when the key or option does not exist.
	ErrNotFound = errors.New("structtag: not found")
	// ErrSyntax is returned when a tag value or a single-quoted string is
	// malformed.
	ErrSyntax = errors.New("structtag: syntax error")
)

// Tag is one key:"value" pair of a struct tag.
type Tag struct {
	Key   string
	Value string
}

// String returns the tag formatted as key:"value".
func (t Tag) String() string {
	return t.Key + ":" + strconv.Quote(t.Value)
}

// Tags is every key:"value" pair of a struct tag, in order.
type Tags []Tag

// Parse parses tag into its key:"value" pairs.
func Parse(tag reflect.StructTag) (Tags, error) {
	var out Tags

	for tag != "" {
		tag = reflect.StructTag(strings.TrimLeft(string(tag), " "))
		if tag == "" {
			break
		}

		// Scan to colon. A space, a quote or a control character is a syntax
		// error. Multi-byte control characters are not checked, as in
		// reflect.StructTag.Lookup.
		i := 0
		for i < len(tag) && tag[i] > ' ' && tag[i] != ':' && tag[i] != '"' && tag[i] != 0x7f {
			i++
		}

		if i == 0 || i+1 >= len(tag) || tag[i] != ':' || tag[i+1] != '"' {
			return nil, fmt.Errorf("%w: %q", ErrUnpairedKey, string(tag))
		}

		key := string(tag[:i])
		tag = tag[i+1:]

		// Scan the quoted value.
		i = 1
		for i < len(tag) && tag[i] != '"' {
			if tag[i] == '\\' {
				i++
			}

			i++
		}

		if i >= len(tag) {
			return nil, fmt.Errorf("%w: %s:%s", ErrUnpairedKey, key, string(tag))
		}

		quoted := string(tag[:i+1])
		tag = tag[i+1:]

		value, err := strconv.Unquote(quoted)
		if err != nil {
			return nil, fmt.Errorf("%w: %s:%s: %w", ErrSyntax, key, quoted, err)
		}

		out = append(out, Tag{Key: key, Value: value})
	}

	return out, nil
}

// StructTag formats t as a reflect.StructTag, pairs separated by a space.
func (t Tags) StructTag() reflect.StructTag {
	parts := opiter.WrapSeq(
		slices.Values(t),
	).
		Map(Tag.String).
		Collect()

	return reflect.StructTag(strings.Join(parts, " "))
}

// Get returns the part of the value of key that option names: the name when
// option is empty, option itself for a bare option, or option:value.
//
// raw is the part as written. unescaped differs from raw when the name is
// single-quoted.
func (t Tags) Get(key, option string) (raw, unescaped string, err error) {
	found := opiter.WrapSeq(
		slices.Values(t),
	).
		Filter(hasKey(key)).
		First()

	if found.IsNone() {
		return "", "", fmt.Errorf("%w: key %q", ErrNotFound, key)
	}

	v := found.Value().Value

	r, err := locate(v, option)
	if err != nil {
		return "", "", err
	}

	raw = v[r.start:r.end]
	if r.quoted {
		return raw, r.unescaped, nil
	}

	return raw, raw, nil
}

// Delete returns a copy of t with the part of key's value that option names
// removed. An empty option removes the name.
func (t Tags) Delete(key, option string) (Tags, error) {
	i := t.index(key)
	if i < 0 {
		return nil, fmt.Errorf("%w: key %q", ErrNotFound, key)
	}

	v := t[i].Value

	r, err := locate(v, option)
	if err != nil {
		return nil, err
	}

	start, end := r.start, r.end
	if option != "" {
		// Take the comma before the option along.
		start--
	}

	v = v[:start] + v[end:]

	out := slices.Clone(t)
	out[i].Value = v

	return out, nil
}

// Add returns a copy of t with option added to key's value, or with value
// as the name when option is empty. A non-empty value of an option is added
// as option:value. A missing key is appended.
//
// An option or a name that already exists is left as is, even when value
// differs.
func (t Tags) Add(key, option, value string) (Tags, error) {
	i := t.index(key)
	if i < 0 {
		return append(slices.Clone(t), Tag{Key: key, Value: part(option, value)}), nil
	}

	v := t[i].Value

	_, err := locate(v, option)
	switch {
	case err == nil:
		return slices.Clone(t), nil
	case !errors.Is(err, ErrNotFound):
		return nil, err
	}

	if option == "" {
		v = value + v
	} else {
		// A value ending in a comma that locate accepted ends in an empty
		// option. Appending after it would leave the empty option in the
		// middle.
		v = strings.TrimSuffix(v, ",") + part(option, value)
	}

	out := slices.Clone(t)
	out[i].Value = v

	return out, nil
}

func (t Tags) index(key string) int {
	return slices.IndexFunc(t, hasKey(key))
}

func hasKey(key string) func(Tag) bool {
	return func(tag Tag) bool { return tag.Key == key }
}

func part(option, value string) string {
	if option == "" {
		return value
	}

	if value == "" {
		return "," + option
	}

	return "," + option + ":" + value
}

type span struct {
	start, end int
	// quoted is true when the name is single-quoted, and unescaped holds it
	// unquoted.
	quoted    bool
	unescaped string
}

// locate finds the part of v that option names, as Get describes.
func locate(v, option string) (span, error) {
	pos := 0

	if v != "" && v[0] != ',' {
		unescaped, n, quoted, err := readName(v)
		if err != nil {
			return span{}, err
		}

		if option == "" {
			return span{0, n, quoted, unescaped}, nil
		}

		pos = n
	}

	for pos < len(v) {
		if v[pos] != ',' {
			return span{}, fmt.Errorf("%w: %q", ErrSyntax, v)
		}

		if pos+1 == len(v) {
			break
		}

		start := pos + 1

		opt, n, err := readOption(v[start:])
		if err != nil {
			return span{}, err
		}

		end := start + n

		if end < len(v) && v[end] == ':' {
			_, n, err := readOption(v[end+1:])
			if err != nil {
				return span{}, err
			}

			end += 1 + n
		}

		if option != "" && opt == option {
			return span{start: start, end: end}, nil
		}

		pos = end
	}

	if option == "" {
		return span{}, fmt.Errorf("%w: name in %q", ErrNotFound, v)
	}

	return span{}, fmt.Errorf("%w: option %q in %q", ErrNotFound, option, v)
}

// readName reads the name at the start of v: any run of characters other
// than a comma, a backslash or a quote, or a single-quoted string.
func readName(v string) (unescaped string, n int, quoted bool, err error) {
	n = strings.IndexAny(v, ",\\'\"`")
	switch {
	case n < 0:
		return v, len(v), false, nil
	case n > 0:
		return v[:n], n, false, nil
	}

	opt, n, err := readOption(v)

	return opt, n, v[0] == '\'', err
}

// readOption reads a Go identifier or a single-quoted string at the start of
// s.
func readOption(s string) (opt string, n int, err error) {
	if s == "" {
		return "", 0, fmt.Errorf("%w: %w", ErrSyntax, io.ErrUnexpectedEOF)
	}

	switch r, _ := utf8.DecodeRuneInString(s); {
	case r == '_' || unicode.IsLetter(r):
		n = len(s) - len(strings.TrimLeftFunc(s, func(r rune) bool {
			return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
		}))

		return s[:n], n, nil
	case r == '\'':
		return unescape(s)
	default:
		return "", 0, fmt.Errorf("%w: unexpected %q", ErrSyntax, s)
	}
}

// unescape reads the single-quoted string at the start of s. Its content
// follows Go's double-quoted string escapes, with \' for a single quote.
func unescape(s string) (unescaped string, n int, err error) {
	buf := []byte{'"'}

	for i := 1; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch r {
		case '\\':
			if i+1 >= len(s) {
				i = len(s)
				continue
			}

			if s[i+1] == '\'' {
				buf = append(buf, '\'')
			} else {
				buf = append(buf, s[i:i+2]...)
			}

			i += 2

			continue
		case '"':
			buf = append(buf, '\\', '"')
		case '\'':
			buf = append(buf, '"')

			out, err := strconv.Unquote(string(buf))
			if err != nil {
				return "", 0, fmt.Errorf("%w: %q: %w", ErrSyntax, s[:i+1], err)
			}

			return out, i + 1, nil
		default:
			buf = append(buf, s[i:i+size]...)
		}

		i += size
	}

	return "", 0, fmt.Errorf("%w: unterminated single-quoted string %q", ErrSyntax, s)
}

// Unescape returns the content of s, a single-quoted string as a name or an
// option can be written.
func Unescape(s string) (string, error) {
	if !strings.HasPrefix(s, "'") {
		return "", fmt.Errorf("%w: %q is not single-quoted", ErrSyntax, s)
	}

	unescaped, n, err := unescape(s)
	if err != nil {
		return "", err
	}

	if n != len(s) {
		return "", fmt.Errorf("%w: trailing characters after position %d", ErrSyntax, n)
	}

	return unescaped, nil
}

// Escape single-quotes s so that it can be written as a name or an option.
// It is the inverse of Unescape.
func Escape(s string) string {
	quoted := strconv.Quote(s)
	return "'" + strings.ReplaceAll(quoted[1:len(quoted)-1], "'", `\'`) + "'"
}
