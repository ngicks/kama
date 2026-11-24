package rule

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
//
// On raw JSON text, as IsRawJson tells, it matches strings and passes a
// value of another kind.
type Regexp struct{}

// Name returns "regexp".
func (Regexp) Name() string { return "regexp" }

// RegexpOptions are the options of //kama:validate regexp=<pattern>.
type RegexpOptions struct {
	// Pattern is the whole arg.
	Pattern string
}

// Parse decodes arg into a pattern that compiles.
func (Regexp) Parse(arg string, _ Resolver) (Rule, error) {
	if arg == "" {
		return nil, argsError(errors.New("want a pattern, as in regexp=\"^[a-z]+$\""))
	}

	if _, err := regexp.Compile(arg); err != nil {
		return nil, argsError(fmt.Errorf("pattern %q does not compile: %w", arg, err))
	}

	return RegexpOptions{Pattern: arg}, nil
}

// Check accepts the types the doc comment of Regexp lists.
func (RegexpOptions) Check(t types.Type) error {
	if !isStringType(t) && !isBytesType(t) {
		return fmt.Errorf("%s is neither a string nor a []byte type", t)
	}

	return nil
}

// Emit calls the helper matching strings or bytes with the variable
// holding the compiled pattern.
func (o RegexpOptions) Emit(e Emitter, t types.Type, v Gen) (Gen, error) {
	re := regexpVarName(o.Pattern)
	e.Declare(re, func(imp Importer) Step {
		return lang.Stmt("var %s = %s.MustCompile(%s)",
			re, imp.Import("regexp"), strconv.Quote(o.Pattern))
	})

	if IsRawJson(t) {
		return rawCall(e, "Pattern", v, lang.Expr("%s", re)), nil
	}

	if isStringType(t) {
		e.Declare(regexpStringName, regexpStringDecl)

		return lang.Expr("%s(%s, %s)",
			regexpStringName, re, convert(t, types.Typ[types.String], "string", v),
		), nil
	}

	e.Declare(regexpBytesName, regexpBytesDecl)

	return lang.Expr("%s(%s, %s)", regexpBytesName, re, convert(t, bytesType, "[]byte", v)), nil
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

var (
	regexpStringDecl = regexpDecl(regexpStringName, "string", "MatchString")
	regexpBytesDecl  = regexpDecl(regexpBytesName, "[]byte", "Match")
)

// regexpDecl returns the declaration of the helper name, which matches its
// argument of type typ with the method match of *regexp.Regexp.
func regexpDecl(name, typ, match string) func(imp Importer) Step {
	return func(imp Importer) Step {
		v := imp.Import(ValidationPath)

		return lang.Func(
			lang.Expr("func %s(%s *%s.Regexp, %s %s) error",
				name, shadow("re"), imp.Import("regexp"), shadow("got"), typ),
			lang.If(
				lang.Expr("!%s.%s(%s)", ref("re"), match, ref("got")),
				lang.Stmt(
					`return %s.New(%s.ErrPattern, "want a match of %%q, got %%q", %s.String(), %s)`,
					v,
					v,
					ref("re"),
					ref("got"),
				),
				nil,
			),
			lang.Stmt("return nil"),
		)
	}
}
