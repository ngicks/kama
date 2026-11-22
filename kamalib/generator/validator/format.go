package validator

import (
	"fmt"
	"go/types"
	"maps"
	"slices"
	"strings"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
)

// Format is the validator //kama:validate:format <name>: the value is a
// string of the named format.
//
// It applies to types of a string underlying type. Formats holds the
// formats by name; DefaultFormats returns the built-in ones. A caller adds
// a format by registering a Format holding it in place of the built-in
// one:
//
//	formats := validator.DefaultFormats()
//	formats["uuid"] = validator.FormatHelper{Name: "kamaValidateFormatUuid", Decl: uuidDecl}
//	registry.Register(validator.Format{Formats: formats})
type Format struct {
	Formats map[string]FormatHelper
}

// FormatHelper is the helper function testing one format.
type FormatHelper struct {
	// Name is the name of the helper, a function of the generated package
	// with the signature func(string) error.
	Name string
	// Decl returns the source of the helper, as Emitter.Declare takes it.
	Decl func(s Spelling) (string, error)
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

// Check accepts the name of one of f.Formats, on string types.
func (f Format) Check(t types.Type, args []dircmnt.KeyValue) error {
	if _, err := f.helper(args); err != nil {
		return argsError(err)
	}

	if !isStringType(t) {
		return fmt.Errorf("%s is not a string type", t)
	}

	return nil
}

// Emit calls the helper of the format.
func (f Format) Emit(e Emitter, t types.Type, v string, args []dircmnt.KeyValue) (string, error) {
	h, err := f.helper(args)
	if err != nil {
		return "", err
	}

	e.Declare(h.Name, h.Decl)

	return h.Name + "(" + convert(t, types.Typ[types.String], "string", v) + ")", nil
}

// helper returns the helper of the format args name.
func (f Format) helper(args []dircmnt.KeyValue) (FormatHelper, error) {
	known := strings.Join(slices.Sorted(maps.Keys(f.Formats)), ", ")

	if len(args) != 1 || args[0].HasValue {
		return FormatHelper{}, fmt.Errorf("want one format name; known: %s", known)
	}

	h, ok := f.Formats[args[0].Key]
	if !ok {
		return FormatHelper{}, fmt.Errorf("unknown format %q; known: %s", args[0].Key, known)
	}

	return h, nil
}

// timeFormatDecl returns the source of the helper name testing that a
// string parses as the layout constant layout of package time.
func timeFormatDecl(name, layout, desc string) func(s Spelling) (string, error) {
	return func(s Spelling) (string, error) {
		v, err := s.Import(validationPath)
		if err != nil {
			return "", err
		}

		tm, err := s.Import("time")
		if err != nil {
			return "", err
		}

		return fmt.Sprintf(`func %[3]s(got string) error {
	if _, err := %[2]s.Parse(%[2]s.%[4]s, got); err != nil {
		return %[1]s.New(%[1]s.ErrFormat, "want format %[5]s, got %%q", got)
	}
	return nil
}`, v, tm, name, layout, desc), nil
	}
}

func urlFormatDecl(s Spelling) (string, error) {
	v, err := s.Import(validationPath)
	if err != nil {
		return "", err
	}

	url, err := s.Import("net/url")
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`func kamaValidateFormatUrl(got string) error {
	if u, err := %[2]s.Parse(got); err != nil || u.Scheme == "" || u.Host == "" {
		return %[1]s.New(%[1]s.ErrFormat, "want format url with a scheme and a host, got %%q", got)
	}
	return nil
}`, v, url), nil
}
