package validator

import (
	"fmt"

	"github.com/ngicks/kama/kamalib/generator/utils/genopt"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// optionSource locates one option for error messages.
type optionSource struct {
	src genopt.Source
	// arg is the item of //kama:validate that sets the option: a validator
	// name, hook=True or hook=False from a directive, hook from an option
	// file, or empty for the mark of a type.
	arg string
	// keys are the keys below the entry of src that the option is spelled
	// with in an option file. arg is the key when keys is nil.
	keys []string
}

// origin locates o: the position of a directive, or the option file and
// the key path of its entry.
func (o optionSource) origin() string { return o.src.Origin(o.arg, o.keys) }

// spelling names the option o sets as its source spells it.
func (o optionSource) spelling() string {
	return o.src.Spelling(GeneratorId().Namespace, GeneratorId().Name, " ", o.arg)
}

// subDirectiveError reports //kama:validate:<sub>, and shows the item of
// //kama:validate that stands for it.
func subDirectiveError(src genopt.Source, sub string, args []expr.Arg) error {
	want := expr.Call{Name: sub, Args: args}.String()
	if (sub == hookWord || sub == preWord || sub == postWord) && len(args) == 1 {
		want = expr.Arg{Name: sub, Value: args[0].Value}.String()
	}

	return fmt.Errorf(
		"%w: %s: %s:%s; validators and hook are args of %s, as in %s %s",
		ErrDirective, src.Pos, GeneratorId().Directive(), sub,
		GeneratorId().Directive(), GeneratorId().Directive(), want,
	)
}

// loadError marks err, an error of a Loader, with the errors of this
// package matching the errors of genopt it wraps.
func loadError(err error) error { return genopt.WrapLoadError(err, ErrOptionFile, ErrDirective) }
