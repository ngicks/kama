package basic

import (
	"fmt"
	"go/types"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// Format is the validator //kama:validate format("<name>"): the value is a
// string of the named format.
//
// It applies to types of a string underlying type and to raw JSON text, as
// IsRawJson tells, where it checks strings and passes a value of another
// kind. Formats holds the
// formats by name; DefaultFormats returns the built-in ones. A caller adds
// a format by registering a Format holding it in place of the built-in
// one:
//
//	formats := basic.DefaultFormats()
//	formats["uuid"] = basic.FormatHelper{Name: "kamaValidateFormatUuid", Decl: uuidDecl}
//	registry.Register(basic.Format{Formats: formats})
type Format struct {
	Formats map[string]FormatHelper
}

// FormatHelper is the helper function testing one format.
type FormatHelper struct {
	// Name is the name of the helper, a function of the generated package
	// with the signature func(string) error.
	Name string
	// Decl returns the declaration of the helper, as Emitter.Declare takes
	// it.
	Decl func(imp ext.Importer) ext.Step
}

// DefaultFormats returns the built-in formats:
//
//   - date: a date as time.DateOnly (2006-01-02) parses it.
//   - date-time: a date and time as time.RFC3339 parses it.
//   - url: a URL that net/url parses, with a scheme and a host.
//   - ipv4: an IPv4 address in dotted decimal, as netip.ParseAddr parses
//     it.
//   - ipv6: an IPv6 address without a zone, as netip.ParseAddr parses it.
//   - uuid: a UUID as RFC 9562 spells it, 32 hexadecimal digits in groups
//     of 8, 4, 4, 4 and 12 separated by hyphens.
func DefaultFormats() map[string]FormatHelper {
	return map[string]FormatHelper{
		"ipv4": {Name: "kamaValidateFormatIpv4", Decl: checkFormatDecl(
			"kamaValidateFormatIpv4", "ipv4", func(imp ext.Importer) ext.Gen {
				return lang.Expr("%s, %s := %s.ParseAddr(%s); %s != nil || !%s.Is4()",
					shadow("a"), shadow("err"), imp.Import("net/netip"), ref("got"),
					ref("err"), ref("a"))
			},
		)},
		"ipv6": {Name: "kamaValidateFormatIpv6", Decl: checkFormatDecl(
			"kamaValidateFormatIpv6", "ipv6", func(imp ext.Importer) ext.Gen {
				return lang.Expr(
					`%s, %s := %s.ParseAddr(%s); %s != nil || !%s.Is6() || %s.Zone() != ""`,
					shadow("a"),
					shadow("err"),
					imp.Import("net/netip"),
					ref("got"),
					ref("err"),
					ref("a"),
					ref("a"),
				)
			},
		)},
		"uuid": {Name: "kamaValidateFormatUuid", Decl: checkFormatDecl(
			"kamaValidateFormatUuid", "uuid", func(imp ext.Importer) ext.Gen {
				str := imp.Import("strings")

				return lang.Expr(
					`len(%s) != 36 || %s.Count(%s, "-") != 4 || `+
						`%s[8] != '-' || %s[13] != '-' || %s[18] != '-' || %s[23] != '-' || `+
						`%s.Trim(%s, "-0123456789abcdefABCDEF") != ""`,
					ref("got"), str, ref("got"), ref("got"), ref("got"), ref("got"), ref("got"),
					str, ref("got"),
				)
			},
		)},
		"date": {Name: "kamaValidateFormatDate", Decl: timeFormatDecl(
			"kamaValidateFormatDate", "DateOnly", "date (2006-01-02)",
		)},
		"date-time": {Name: "kamaValidateFormatDateTime", Decl: timeFormatDecl(
			"kamaValidateFormatDateTime", "RFC3339", "date-time (RFC 3339)",
		)},
		"url": {Name: "kamaValidateFormatUrl", Decl: urlFormatDecl},
	}
}

// Name returns "format".
func (Format) Name() string { return "format" }

// FormatArgs are the args of format("<name>"), which Parse reads and Call spells.
type FormatArgs struct {
	Name string `json:"name"`
}

// Call returns the call of format spelling a.
func (a FormatArgs) Call() (expr.Call, error) { return formatParams.Call(Format{}.Name(), a) }

var formatParams = ext.Params{Positional: []string{"name"}}

// FormatOptions are the options of //kama:validate format("<name>").
type FormatOptions struct {
	// Name is the whole arg, the name of the format.
	Name string
	// Helper is the helper f.Formats holds under Name.
	Helper FormatHelper
}

// Parse decodes the arg of c into the name of one of f.Formats and its
// helper.
func (f Format) Parse(c expr.Call, r ext.Resolver) (ext.Rule, error) {
	known := strings.Join(slices.Sorted(maps.Keys(f.Formats)), ", ")

	var args FormatArgs

	if _, err := formatParams.Bind(r, c, &args); err != nil {
		return nil, err
	}

	if args.Name == "" {
		return nil, argsError(
			fmt.Errorf("want one format name, as in format(\"date\"); known: %s", known),
		)
	}

	h, ok := f.Formats[args.Name]
	if !ok {
		return nil, argsError(fmt.Errorf("unknown format %q; known: %s", args.Name, known))
	}

	return FormatOptions{Name: args.Name, Helper: h}, nil
}

// Check accepts string types and raw JSON text.
func (FormatOptions) Check(t types.Type) error {
	if !isStringType(t) && !ext.IsRawJson(t) {
		return fmt.Errorf("%s is neither a string type nor raw JSON text", t)
	}

	return nil
}

// Emit calls the helper of the format, through rawjson.Format on raw JSON
// text.
func (o FormatOptions) Emit(e ext.Emitter, t types.Type, v ext.Gen) (ext.Gen, error) {
	e.Declare(o.Helper.Name, o.Helper.Decl)

	if ext.IsRawJson(t) {
		return rawCall(e, "Format", v, lang.Expr("%s", o.Helper.Name)), nil
	}

	return lang.Expr("%s(%s)", o.Helper.Name, convert(t, types.Typ[types.String], "string", v)), nil
}

// timeFormatDecl returns the declaration of the helper name testing that a
// string parses as the layout constant layout of package time.
func timeFormatDecl(name, layout, desc string) func(imp ext.Importer) ext.Step {
	return func(imp ext.Importer) ext.Step {
		v, tm := imp.Import(ext.ValidationPath), imp.Import("time")

		return lang.Func(
			lang.Expr("func %s(%s string) error", name, shadow("got")),
			lang.If(
				lang.Expr("_, %s := %s.Parse(%s.%s, %s); %s != nil",
					shadow("err"), tm, tm, layout, ref("got"), ref("err")),
				lang.Stmt("return %s.New(%s.ErrFormat, %s, %s)",
					v, v, strconv.Quote("want format "+desc+", got %q"), ref("got")),
				nil,
			),
			lang.Stmt("return nil"),
		)
	}
}

// checkFormatDecl returns the declaration of the helper name rejecting a
// string for which the if header failed gives true, which reads the string
// as got.
func checkFormatDecl(
	name, desc string,
	failed func(imp ext.Importer) ext.Gen,
) func(imp ext.Importer) ext.Step {
	return func(imp ext.Importer) ext.Step {
		v := imp.Import(ext.ValidationPath)

		return lang.Func(
			lang.Expr("func %s(%s string) error", name, shadow("got")),
			lang.If(
				failed(imp),
				lang.Stmt("return %s.New(%s.ErrFormat, %s, %s)",
					v, v, strconv.Quote("want format "+desc+", got %q"), ref("got")),
				nil,
			),
			lang.Stmt("return nil"),
		)
	}
}

func urlFormatDecl(imp ext.Importer) ext.Step {
	v := imp.Import(ext.ValidationPath)

	return lang.Func(
		lang.Expr("func kamaValidateFormatUrl(%s string) error", shadow("got")),
		lang.If(
			lang.Expr(`%s, %s := %s.Parse(%s); %s != nil || %s.Scheme == "" || %s.Host == ""`,
				shadow("u"), shadow("err"), imp.Import("net/url"), ref("got"),
				ref("err"), ref("u"), ref("u")),
			lang.Stmt(
				`return %s.New(%s.ErrFormat, "want format url with a scheme and a host, got %%q", %s)`,
				v,
				v,
				ref("got"),
			),
			nil,
		),
		lang.Stmt("return nil"),
	)
}
