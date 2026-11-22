package validator

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
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

// hookDirective is the name of //kama:validate:hook. Every other name after
// //kama:validate: on a field names a validator.
const hookDirective = "hook"

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
		return fmt.Errorf(
			"%w: %s: //kama:validate:%s on a free-floating comment; it applies to types and fields",
			ErrDirective, src.Pos, sub,
		)
	}

	words, ok := bareWords(kv)
	if !ok || len(words) == 0 {
		return fmt.Errorf(
			"%w: %s: unknown args %q on a free-floating comment; want file or package",
			ErrDirective, src.Pos, spellArgs(kv),
		)
	}

	for _, w := range words {
		switch w {
		case "file":
			o.file = true
		case "package":
			o.AllTypes = true
		default:
			return fmt.Errorf(
				"%w: %s: unknown args %q on a free-floating comment; want file or package",
				ErrDirective, src.Pos, spellArgs(kv),
			)
		}
	}

	return nil
}

// TypeOptions are the options of a type.
type TypeOptions struct {
	// Ignore stands for //kama:validate ignore.
	Ignore bool `json:"ignore"`
	// Hook stands for //kama:validate:hook on (true) or off (false). Nil
	// leaves the default.
	Hook *bool `json:"hook"`
	// marked stands for //kama:validate, or for an option file entry of the
	// type without ignore.
	marked bool
	// markSrc locates marked or Ignore, and hookSrc Hook.
	markSrc, hookSrc optionSource
}

// UnmarshalKeyValue decodes a directive on a type or a type group.
func (o *TypeOptions) UnmarshalKeyValue(
	src genopt.Source,
	sub string,
	kv []dircmnt.KeyValue,
) error {
	switch sub {
	case "":
		words, ok := bareWords(kv)

		switch {
		case ok && len(words) == 0:
			o.marked = true
		case ok && slices.Equal(words, []string{"ignore"}):
			o.Ignore = true
		default:
			return fmt.Errorf(
				"%w: %s: unknown args %q on %s; want none or ignore",
				ErrDirective, src.Pos, spellArgs(kv), onTarget(src.Target),
			)
		}

		o.markSrc = optionSource{src: src}
	case hookDirective:
		on, err := hookArg(kv)
		if err != nil {
			return fmt.Errorf("%w: %s: %w on %s", ErrDirective, src.Pos, err, onTarget(src.Target))
		}

		o.Hook = &on
		o.hookSrc = optionSource{src: src, sub: hookDirective}
	default:
		return fmt.Errorf(
			"%w: %s: //kama:validate:%s on %s; validators apply to struct fields only",
			ErrDirective, src.Pos, sub, onTarget(src.Target),
		)
	}

	return nil
}

// SetSource records src, the option file entry of the type. Listing a type
// marks it unless Ignore is set.
func (o *TypeOptions) SetSource(src genopt.Source) {
	o.marked = !o.Ignore
	o.markSrc = optionSource{src: src}

	if o.Ignore {
		o.markSrc.keys = []string{"ignore"}
	}

	if o.Hook != nil {
		o.hookSrc = optionSource{src: src, sub: hookDirective}
	}
}

// FieldOptions are the options of a struct field.
type FieldOptions struct {
	// Hook stands for //kama:validate:hook on (true) or off (false). Nil
	// leaves the default of the type.
	Hook *bool `json:"hook"`
	// Validators stand for the //kama:validate:<name> directives of the
	// field, in the order written.
	Validators []ValidatorOptions `json:"validators"`
	hookSrc    optionSource
}

// UnmarshalKeyValue decodes a directive on a field.
func (o *FieldOptions) UnmarshalKeyValue(
	src genopt.Source,
	sub string,
	kv []dircmnt.KeyValue,
) error {
	switch sub {
	case "":
		return fmt.Errorf(
			"%w: %s: //kama:validate on field %s; mark the type instead",
			ErrDirective, src.Pos, fieldName(src.Target),
		)
	case hookDirective:
		on, err := hookArg(kv)
		if err != nil {
			return fmt.Errorf(
				"%w: %s: %w on field %s", ErrDirective, src.Pos, err, fieldName(src.Target),
			)
		}

		o.Hook = &on
		o.hookSrc = optionSource{src: src, sub: hookDirective}
	default:
		o.Validators = append(o.Validators, ValidatorOptions{
			Name: sub,
			Args: spellArgs(kv),
			kv:   kv,
			src:  optionSource{src: src, sub: sub},
		})
	}

	return nil
}

// SetSource records src, the option file entry of the field.
func (o *FieldOptions) SetSource(src genopt.Source) {
	if o.Hook != nil {
		o.hookSrc = optionSource{src: src, sub: hookDirective}
	}

	for i := range o.Validators {
		o.Validators[i].src = optionSource{
			src: src, sub: o.Validators[i].Name, keys: []string{"validators", strconv.Itoa(i)},
		}
	}
}

// ValidatorOptions are one validator of a field: //kama:validate:<Name>
// <Args>.
type ValidatorOptions struct {
	Name string `json:"name"`
	// Args are the args in the grammar of dircmnt.ParseArgsKeyValue.
	Args string `json:"args"`
	// kv are Args parsed.
	kv  []dircmnt.KeyValue
	src optionSource
}

// UnmarshalJSONFrom decodes a validator entry of an option file and parses
// its args, so that bad args fail where they are written.
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

	kv, err := dircmnt.ParseArgsKeyValue(v.Args)
	if err != nil {
		return fmt.Errorf("args of validator %s: %w", v.Name, err)
	}

	v.kv = kv
	*o = ValidatorOptions(v)

	return nil
}

// hookArg decodes the args of //kama:validate:hook.
func hookArg(kv []dircmnt.KeyValue) (bool, error) {
	words, ok := bareWords(kv)
	if ok && len(words) == 1 {
		switch words[0] {
		case "on":
			return true, nil
		case "off":
			return false, nil
		}
	}

	return false, fmt.Errorf("args %q of //kama:validate:hook; want on or off", spellArgs(kv))
}
