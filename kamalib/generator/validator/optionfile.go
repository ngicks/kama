package validator

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strconv"

	"github.com/ngicks/kama/kamalib/generator/utils/genopt"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
)

// OptionFileVersion is the only version of the option file format.
const OptionFileVersion = 1

// LoadedOptions are the options of Generate. The Loader of NewLoader loads
// them.
type LoadedOptions = genopt.Loaded[PackageOptions, TypeOptions, FieldOptions]

// NewLoader returns the loader of the options of the generator. optionFile
// finds the central option files relative to the root; none are read when
// it is nil. A package without //kama:option-file validate=<path> reads
// kama_validate.yaml, .yml, .json or .toml in its directory.
func NewLoader(
	optionFile fileloader.Input,
) genopt.Loader[PackageOptions, TypeOptions, FieldOptions] {
	return genopt.Loader[PackageOptions, TypeOptions, FieldOptions]{
		ToolName:  toolName,
		Generator: directive,
		Version:   OptionFileVersion,
		File:      optionFile,
		PackageFile: fileloader.PathInput{
			Base: packageOptionFileBase,
			Formats: []fileloader.Format{
				fileloader.FormatYaml,
				fileloader.FormatJson,
				fileloader.FormatToml,
			},
		},
	}
}

// packageOptionFileBase is the name of the package option file, without
// extension.
const packageOptionFileBase = "kama_validate"

// The words of //kama:validate that name no validator. Generate rejects a
// registry holding a validator named by one of them.
const (
	hookWord    = "hook"
	ignoreWord  = "ignore"
	fileWord    = "file"
	packageWord = "package"
)

// PackageOptions are the options of a package, from the package entry of an
// option file or from the free-floating directives of a file.
type PackageOptions struct {
	// AllTypes stands for //kama:validate package.
	AllTypes bool `json:"all_types"`
	// file stands for //kama:validate file, which option files lack.
	file bool
}

// UnmarshalKeyValue decodes a directive in a free-floating comment.
func (o *PackageOptions) UnmarshalKeyValue(
	src genopt.Source,
	sub string,
	kv []dircmnt.KeyValue,
) error {
	if sub != "" {
		return subDirectiveError(src, sub, kv)
	}

	unknown := fmt.Errorf(
		"%w: %s: unknown args %q on a free-floating comment; want file or package",
		ErrDirective, src.Pos, dircmnt.SpellArgs(kv),
	)

	words, ok := dircmnt.BareWords(kv)
	if !ok || len(words) == 0 {
		return unknown
	}

	for _, w := range words {
		switch w {
		case fileWord:
			o.file = true
		case packageWord:
			o.AllTypes = true
		default:
			return unknown
		}
	}

	return nil
}

// TypeOptions are the options of a type.
type TypeOptions struct {
	// Ignore stands for //kama:validate ignore.
	Ignore bool `json:"ignore"`
	// Hook stands for //kama:validate hook=on (true) or hook=off (false). Nil
	// leaves the default.
	Hook *bool `json:"hook"`
	// marked stands for //kama:validate without ignore, or for an option
	// file entry of the type without ignore.
	marked bool
	// markSrc locates marked or Ignore, and hookSrc Hook.
	markSrc, hookSrc optionSource
}

// UnmarshalKeyValue decodes a directive on a type or a type group. Every
// directive without ignore marks the type, as an option file entry does.
// genopt joins the lines of a target into one directive, so a line holding
// only hook=on cannot be told from one following a bare //kama:validate.
func (o *TypeOptions) UnmarshalKeyValue(
	src genopt.Source,
	sub string,
	kv []dircmnt.KeyValue,
) error {
	if sub != "" {
		return subDirectiveError(src, sub, kv)
	}

	for _, a := range kv {
		switch {
		case a.Key == ignoreWord && !a.HasValue:
			o.Ignore = true
		case a.Key == hookWord:
			if o.Hook != nil {
				return fmt.Errorf(
					"%w: %s: hook given twice on %s", ErrDirective, src.Pos, onTarget(src.Target),
				)
			}

			on, err := hookValue(a)
			if err != nil {
				return fmt.Errorf(
					"%w: %s: %w on %s", ErrDirective, src.Pos, err, onTarget(src.Target),
				)
			}

			o.Hook = &on
			o.hookSrc = optionSource{src: src, arg: a.String()}
		default:
			return fmt.Errorf(
				"%w: %s: unknown arg %q on %s; want ignore or hook=on|off;"+
					" validators apply to struct fields only",
				ErrDirective, src.Pos, a.String(), onTarget(src.Target),
			)
		}
	}

	o.marked = !o.Ignore
	o.markSrc = optionSource{src: src}

	return nil
}

// SetSource records src, the option file entry of the type. Listing a type
// marks it unless Ignore is set.
func (o *TypeOptions) SetSource(src genopt.Source) {
	o.marked = !o.Ignore
	o.markSrc = optionSource{src: src}

	if o.Ignore {
		o.markSrc.keys = []string{ignoreWord}
	}

	if o.Hook != nil {
		o.hookSrc = optionSource{src: src, arg: hookWord}
	}
}

// FieldOptions are the options of a struct field.
type FieldOptions struct {
	// Hook stands for //kama:validate hook=on (true) or hook=off (false). Nil
	// leaves the default of the type.
	Hook *bool `json:"hook"`
	// Validators stand for the validators //kama:validate names on the
	// field, in the order written.
	Validators []ValidatorOptions `json:"validators"`
	hookSrc    optionSource
}

// UnmarshalKeyValue decodes a directive on a field. Each arg names a
// validator, except hook.
func (o *FieldOptions) UnmarshalKeyValue(
	src genopt.Source,
	sub string,
	kv []dircmnt.KeyValue,
) error {
	if sub != "" {
		return subDirectiveError(src, sub, kv)
	}

	if len(kv) == 0 {
		return fmt.Errorf(
			"%w: %s: //kama:validate on field %s names no validator;"+
				" mark the type in its doc comment instead",
			ErrDirective, src.Pos, fieldName(src.Target),
		)
	}

	for _, a := range kv {
		if a.Key != hookWord {
			o.Validators = append(o.Validators, ValidatorOptions{
				Name: a.Key,
				Args: a.Value,
				src:  optionSource{src: src, arg: a.Key},
			})

			continue
		}

		if o.Hook != nil {
			return fmt.Errorf(
				"%w: %s: hook given twice on field %s",
				ErrDirective,
				src.Pos,
				fieldName(src.Target),
			)
		}

		on, err := hookValue(a)
		if err != nil {
			return fmt.Errorf(
				"%w: %s: %w on field %s", ErrDirective, src.Pos, err, fieldName(src.Target),
			)
		}

		o.Hook = &on
		o.hookSrc = optionSource{src: src, arg: a.String()}
	}

	return nil
}

// SetSource records src, the option file entry of the field.
func (o *FieldOptions) SetSource(src genopt.Source) {
	if o.Hook != nil {
		o.hookSrc = optionSource{src: src, arg: hookWord}
	}

	for i := range o.Validators {
		o.Validators[i].src = optionSource{
			src: src, arg: o.Validators[i].Name, keys: []string{"validators", strconv.Itoa(i)},
		}
	}
}

// ValidatorOptions are one validator of a field: the arg Name=Args of
// //kama:validate, or Name alone when Args is empty.
type ValidatorOptions struct {
	Name string `json:"name"`
	// Args is the one argument of the validator, unparsed. Empty stands for
	// none.
	Args string `json:"args"`
	src  optionSource
}

// UnmarshalJSONFrom decodes a validator entry of an option file and rejects
// one without a name where it is written.
func (o *ValidatorOptions) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	// fields is ValidatorOptions without its methods.
	type fields ValidatorOptions

	var v fields
	if err := json.UnmarshalDecode(dec, &v); err != nil {
		return err
	}

	if v.Name == "" {
		return errors.New("validator without name")
	}

	*o = ValidatorOptions(v)

	return nil
}

// hookValue decodes the arg hook=on or hook=off.
func hookValue(a dircmnt.KeyValue) (bool, error) {
	if a.HasValue {
		switch a.Value {
		case "on":
			return true, nil
		case "off":
			return false, nil
		}
	}

	return false, fmt.Errorf(
		"arg %q of //kama:validate; want hook=on or hook=off", a.String(),
	)
}
