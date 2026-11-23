package validator

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/types"
	"regexp"
	"strconv"
)

// Regexp is the validator //kama:validate regexp=<pattern>: the value
// matches the RE2 pattern, as package regexp reads it. The whole argument
// is the pattern, so a quoted pattern may hold spaces, = and quotes.
//
// It applies to types of a string underlying type and to types of the
// underlying type []byte. The pattern is compiled once, into a
// package-level variable of the generated package.
type Regexp struct{}

// Name returns "regexp".
func (Regexp) Name() string { return "regexp" }

// Check accepts a pattern that compiles, on the types the doc comment of
// Regexp lists.
func (Regexp) Check(t types.Type, pattern string) error {
	if pattern == "" {
		return argsError(errors.New("want a pattern, as in regexp=\"^[a-z]+$\""))
	}

	if _, err := regexp.Compile(pattern); err != nil {
		return argsError(fmt.Errorf("pattern %q does not compile: %w", pattern, err))
	}

	if !isStringType(t) && !isBytesType(t) {
		return fmt.Errorf("%s is neither a string nor a []byte type", t)
	}

	return nil
}

// Emit calls the helper matching strings or bytes with the variable
// holding the compiled pattern.
func (Regexp) Emit(e Emitter, t types.Type, v string, pattern string) (string, error) {
	re := regexpVarName(pattern)
	e.Declare(re, func(s Spelling) (string, error) {
		pkg, err := s.Import("regexp")
		if err != nil {
			return "", err
		}

		return "var " + re + " = " + pkg + ".MustCompile(" + strconv.Quote(pattern) + ")", nil
	})

	if isStringType(t) {
		e.Declare(regexpStringName, regexpStringDecl)

		return regexpStringName + "(" + re + ", " + convert(
				t,
				types.Typ[types.String],
				"string",
				v,
			) + ")",
			nil
	}

	e.Declare(regexpBytesName, regexpBytesDecl)

	return regexpBytesName + "(" + re + ", " + convert(t, bytesType, "[]byte", v) + ")", nil
}

// regexpVarName names the variable holding pattern compiled after the
// first 5 bytes of its SHA-256, so that one pattern gets one variable per
// package.
func regexpVarName(pattern string) string {
	sum := sha256.Sum256([]byte(pattern))
	return "kamaValidateRegexp_" + hex.EncodeToString(sum[:5])
}

// bytesType is []byte.
var bytesType = types.NewSlice(types.Typ[types.Byte])

// isBytesType reports whether the underlying type of t is []byte.
func isBytesType(t types.Type) bool {
	return types.Identical(t.Underlying(), bytesType)
}

const (
	regexpStringName = "kamaValidateRegexpString"
	regexpBytesName  = "kamaValidateRegexpBytes"
)

func regexpStringDecl(s Spelling) (string, error) {
	return regexpDecl(s, `func kamaValidateRegexpString(re *%[2]s.Regexp, got string) error {
	if !re.MatchString(got) {
		return %[1]s.New(%[1]s.ErrPattern, "want a match of %%q, got %%q", re.String(), got)
	}
	return nil
}`)
}

func regexpBytesDecl(s Spelling) (string, error) {
	return regexpDecl(s, `func kamaValidateRegexpBytes(re *%[2]s.Regexp, got []byte) error {
	if !re.Match(got) {
		return %[1]s.New(%[1]s.ErrPattern, "want a match of %%q, got %%q", re.String(), got)
	}
	return nil
}`)
}

// regexpDecl formats src with the identifiers of package validation and of
// package regexp.
func regexpDecl(s Spelling, src string) (string, error) {
	v, err := s.Import(validationPath)
	if err != nil {
		return "", err
	}

	re, err := s.Import("regexp")
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(src, v, re), nil
}
