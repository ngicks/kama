package rule

import (
	"fmt"
	"go/types"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Format is the validator //kama:validate format=<name>: the value is a
// string of the named format.
//
// It applies to types of a string underlying type. Formats holds the
// formats by name; DefaultFormats returns the built-in ones. A caller adds
// a format by registering a Format holding it in place of the built-in
// one:
//
//	formats := rule.DefaultFormats()
//	formats["uuid"] = rule.FormatHelper{Name: "kamaValidateFormatUuid", Decl: uuidDecl}
//	registry.Register(rule.Format{Formats: formats})
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
	Decl func(imp Importer) Step
}

// DefaultFormats returns the built-in formats:
//
//   - date: a date as time.DateOnly (2006-01-02) parses it.
//   - date-time: a date and time as time.RFC3339 parses it.
//   - url: a URL that net/url parses, with a scheme and a host.
func DefaultFormats() map[string]FormatHelper {
	return map[string]FormatHelper{
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

// FormatOptions are the options of //kama:validate format=<name>.
type FormatOptions struct {
	// Name is the whole arg, the name of the format.
	Name string
	// Helper is the helper f.Formats holds under Name.
	Helper FormatHelper
}

// Parse decodes arg into the name of one of f.Formats and its helper.
func (f Format) Parse(arg string) (Rule, error) {
	known := strings.Join(slices.Sorted(maps.Keys(f.Formats)), ", ")

	if arg == "" {
		return nil, argsError(fmt.Errorf("want one format name; known: %s", known))
	}

	h, ok := f.Formats[arg]
	if !ok {
		return nil, argsError(fmt.Errorf("unknown format %q; known: %s", arg, known))
	}

	return FormatOptions{Name: arg, Helper: h}, nil
}

// Check accepts string types.
func (FormatOptions) Check(t types.Type) error {
	if !isStringType(t) {
		return fmt.Errorf("%s is not a string type", t)
	}

	return nil
}

// Emit calls the helper of the format.
func (o FormatOptions) Emit(e Emitter, t types.Type, v Gen) (Gen, error) {
	e.Declare(o.Helper.Name, o.Helper.Decl)

	return lang.Expr("%s(%s)", o.Helper.Name, convert(t, types.Typ[types.String], "string", v)), nil
}

// timeFormatDecl returns the declaration of the helper name testing that a
// string parses as the layout constant layout of package time.
func timeFormatDecl(name, layout, desc string) func(imp Importer) Step {
	return func(imp Importer) Step {
		v, tm := imp.Import(ValidationPath), imp.Import("time")

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

func urlFormatDecl(imp Importer) Step {
	v := imp.Import(ValidationPath)

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
