package validator

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"go/token"
	"strconv"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/genopt"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/unknownkey"
)

// OptionFileVersion is the only version of the option file format.
const OptionFileVersion = 1

// LoadedOptions are the options of Generate. The Loader of NewLoader loads
// them.
type LoadedOptions = genopt.Loaded[PackageOptions, TypeOptions, FieldOptions]

// NewLoader returns the loader of the options of the generator. optionFile
// finds the central option files relative to the root; none are read when
// it is nil. A package without //kama:option-file validate=<path> reads
// kama_validate.toml, .yaml, .yml or .json in its directory.
func NewLoader(
	optionFile fileloader.Input,
) genopt.Loader[PackageOptions, TypeOptions, FieldOptions] {
	return base.NewOptionLoader[PackageOptions, TypeOptions, FieldOptions](
		GeneratorId(), OptionFileVersion, optionFile,
	)
}

// The words of //kama:validate that name no validator. Generate rejects a
// registry holding a validator named by one of them.
const (
	hookWord    = "hook"
	preWord     = "pre"
	postWord    = "post"
	ignoreWord  = "ignore"
	decodedWord = "decoded"
	fileWord    = "file"
	packageWord = "package"
)

// PackageOptions are the options of a package, from the package entry of an
// option file or from the free-floating directives of a file.
type PackageOptions struct {
	// AllTypes stands for //kama:validate package.
	AllTypes bool `json:"all_types"`
	// Unknown captures the keys of an option file entry that name no option.
	Unknown map[string]any `json:",embed"`
	// file stands for //kama:validate file, which option files lack.
	file bool
}

// UnknownKeys returns the keys of the option file entry that name no
// option.
func (o *PackageOptions) UnknownKeys() [][]string { return unknownkey.Paths(o.Unknown) }

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
	// Decoded stands for //kama:validate decoded: decoding a value of the
	// type validates it, so ValidateShallow takes a decoded value of it as
	// valid.
	Decoded bool `json:"decoded"`
	// Hook stands for //kama:validate hook=on (true) or hook=off (false). Nil
	// leaves the default.
	Hook *bool `json:"hook"`
	// Pre stands for //kama:validate pre=<func>: the package-level function
	// Validate calls first. Empty stands for none.
	Pre string `json:"pre"`
	// Post stands for //kama:validate post=<func>: the package-level
	// function Validate calls last, whatever happened before. Empty stands
	// for none.
	Post string `json:"post"`
	// Validators stand for the validators //kama:validate names in the doc
	// comment of the type, in the order written. They validate the value of
	// the type itself.
	Validators []ValidatorOptions `json:"validators"`
	// Unknown captures the keys of an option file entry that name no option.
	Unknown map[string]any `json:",embed"`
	// marked stands for //kama:validate without ignore, or for an option
	// file entry of the type without ignore.
	marked bool
	// markSrc locates marked or Ignore, hookSrc Hook, preSrc Pre and postSrc
	// Post.
	markSrc, hookSrc, preSrc, postSrc optionSource
}

// UnmarshalKeyValue decodes a directive on a type or a type group. Every
// directive without ignore marks the type, as an option file entry does.
// genopt joins the lines of a target into one directive, so a line holding
// only hook=on cannot be told from one following a bare //kama:validate.
// Each arg other than ignore, decoded, hook, pre and post names a validator.
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
		case a.Key == ignoreWord && !a.HasValue && !a.Call:
			o.Ignore = true
		case a.Key == decodedWord && !a.HasValue && !a.Call:
			o.Decoded = true
		case a.Key == ignoreWord || a.Key == decodedWord:
			return fmt.Errorf(
				"%w: %s: unknown arg %q on %s; want %s without a value",
				ErrDirective, src.Pos, a.String(), src.Target.Describe(), a.Key,
			)
		case a.Key == hookWord:
			if o.Hook != nil {
				return fmt.Errorf(
					"%w: %s: hook given twice on %s", ErrDirective, src.Pos, src.Target.Describe(),
				)
			}

			on, err := hookValue(a)
			if err != nil {
				return fmt.Errorf(
					"%w: %s: %w on %s", ErrDirective, src.Pos, err, src.Target.Describe(),
				)
			}

			o.Hook = &on
			o.hookSrc = optionSource{src: src, arg: a.String()}
		case a.Key == preWord:
			if err := setFunc(&o.Pre, &o.preSrc, src, a); err != nil {
				return err
			}
		case a.Key == postWord:
			if err := setFunc(&o.Post, &o.postSrc, src, a); err != nil {
				return err
			}
		default:
			o.Validators = append(o.Validators, ValidatorOptions{
				Name: a.Key,
				Args: a.Arg(),
				src:  optionSource{src: src, arg: a.Key},
			})
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

	if o.Pre != "" {
		o.preSrc = optionSource{src: src, arg: preWord}
	}

	if o.Post != "" {
		o.postSrc = optionSource{src: src, arg: postWord}
	}

	setValidatorSources(o.Validators, src)
}

// OverlayOn returns lower with the functions of o set on it when o, an
// option file entry, sets pre or post and nothing else, and lower sets
// neither of those o sets. A type then keeps the validators a generator
// wrote into its doc comment, and an option file adds its own checks
// beside them.
func (o TypeOptions) OverlayOn(lower TypeOptions) (TypeOptions, bool) {
	onlyFuncs := !o.Ignore && !o.Decoded && o.Hook == nil && len(o.Validators) == 0 &&
		(o.Pre != "" || o.Post != "")
	if !onlyFuncs || o.Pre != "" && lower.Pre != "" || o.Post != "" && lower.Post != "" {
		return TypeOptions{}, false
	}

	if o.Pre != "" {
		lower.Pre, lower.preSrc = o.Pre, o.preSrc
	}

	if o.Post != "" {
		lower.Post, lower.postSrc = o.Post, o.postSrc
	}

	return lower, true
}

// UnknownKeys returns the keys of the option file entry that name no
// option, those of each validator included.
func (o *TypeOptions) UnknownKeys() [][]string {
	return validatorUnknownKeys(o.Validators, unknownkey.Paths(o.Unknown))
}

// FieldOptions are the options of a struct field.
type FieldOptions struct {
	// Hook stands for //kama:validate hook=on (true) or hook=off (false). Nil
	// leaves the default of the type.
	Hook *bool `json:"hook"`
	// Validators stand for the validators //kama:validate names on the
	// field, in the order written.
	Validators []ValidatorOptions `json:"validators"`
	// Unknown captures the keys of an option file entry that name no option.
	Unknown map[string]any `json:",embed"`
	hookSrc optionSource
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
			ErrDirective, src.Pos, src.Target.FieldName(),
		)
	}

	for _, a := range kv {
		if a.Key == preWord || a.Key == postWord {
			return fmt.Errorf(
				"%w: %s: %s on field %s; write it in the doc comment of the type",
				ErrDirective, src.Pos, a.Key, src.Target.FieldName(),
			)
		}

		if a.Key != hookWord {
			o.Validators = append(o.Validators, ValidatorOptions{
				Name: a.Key,
				Args: a.Arg(),
				src:  optionSource{src: src, arg: a.Key},
			})

			continue
		}

		if o.Hook != nil {
			return fmt.Errorf(
				"%w: %s: hook given twice on field %s",
				ErrDirective,
				src.Pos,
				src.Target.FieldName(),
			)
		}

		on, err := hookValue(a)
		if err != nil {
			return fmt.Errorf(
				"%w: %s: %w on field %s", ErrDirective, src.Pos, err, src.Target.FieldName(),
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

	setValidatorSources(o.Validators, src)
}

// UnknownKeys returns the keys of the option file entry that name no
// option, those of each validator included.
func (o *FieldOptions) UnknownKeys() [][]string {
	return validatorUnknownKeys(o.Validators, unknownkey.Paths(o.Unknown))
}

// setValidatorSources records src, the option file entry listing vs, in
// each of vs.
func setValidatorSources(vs []ValidatorOptions, src genopt.Source) {
	for i := range vs {
		vs[i].src = optionSource{
			src: src, arg: vs[i].Name, keys: []string{"validators", strconv.Itoa(i)},
		}
	}
}

// validatorUnknownKeys returns keys followed by the unknown keys of each
// of vs below the validators key.
func validatorUnknownKeys(vs []ValidatorOptions, keys [][]string) [][]string {
	for i, v := range vs {
		keys = append(keys, unknownkey.Paths(v.Unknown, "validators", strconv.Itoa(i))...)
	}

	return keys
}

// ValidatorOptions are one validator of a field or a type: the arg
// Name=Args or the call Name(Args) of //kama:validate, or Name alone when
// Args is empty.
type ValidatorOptions struct {
	Name string `json:"name"`
	// Args is the one argument of the validator, unparsed. Empty stands for
	// none. For a call it holds the args between the parentheses as
	// dircmnt.SpellCallArgs spells them, as in cmp=">=10", const="0" for
	// or(cmp=">=10", const="0").
	Args string `json:"args"`
	// Unknown captures the keys of an option file entry that name no
	// option.
	Unknown map[string]any `json:",embed"`
	src     optionSource
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

// setFunc sets *dst, the function the arg a of the directive at src names,
// and *dstSrc, its source. a names one function, given once on the type.
func setFunc(dst *string, dstSrc *optionSource, src genopt.Source, a dircmnt.KeyValue) error {
	if *dst != "" {
		return fmt.Errorf(
			"%w: %s: %s given twice on %s", ErrDirective, src.Pos, a.Key, src.Target.Describe(),
		)
	}

	if !a.HasValue || a.Call || !token.IsIdentifier(a.Value) {
		return fmt.Errorf(
			"%w: %s: arg %q of //kama:validate on %s; want %s=<function name>",
			ErrDirective, src.Pos, a.String(), src.Target.Describe(), a.Key,
		)
	}

	*dst = a.Value
	*dstSrc = optionSource{src: src, arg: a.String()}

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
