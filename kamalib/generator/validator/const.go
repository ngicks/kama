package validator

import (
	"errors"
	"fmt"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
)

// Const is the validator //kama:validate const="<v1> <v2> ...": the value
// equals one of the listed values, as in const="1 2 3" or
// const=`"red" "dark blue"`.
//
// It applies to types of a bool, numeric or string underlying type. Each
// token of the argument is a Go literal of the type. A string type takes
// quoted strings only, and any other type takes bare literals only, so
// that const=`"1"` and const=1 do not mean the same.
type Const struct{}

// Name returns "const".
func (Const) Name() string { return "const" }

// Check accepts one or more values of t, on the types the doc comment of
// Const lists.
func (Const) Check(t types.Type, arg string) error {
	args, err := argTokens(arg)
	if err != nil {
		return err
	}

	if len(args) == 0 {
		return argsError(errors.New("want one or more values"))
	}

	for _, kv := range args {
		if kv.HasValue {
			return argsError(fmt.Errorf(
				"arg %q is a key=value pair; quote a value holding =",
				kv.String(),
			))
		}
	}

	info := basicInfo(t)
	if info&(types.IsBoolean|types.IsNumeric|types.IsString) == 0 {
		return fmt.Errorf("%s is not a bool, numeric or string type", t)
	}

	for _, kv := range args {
		if info&types.IsString != 0 && !kv.KeyQuoted {
			return fmt.Errorf(
				"%s is no value of %s; want a quoted string, as in %q", kv.Key, t, kv.Key,
			)
		}

		if _, err := literalValue(t, constLiteral(kv)); err != nil {
			return err
		}
	}

	return nil
}

// Emit calls kamaValidateConst with the values.
func (Const) Emit(e Emitter, _ types.Type, v string, arg string) (string, error) {
	args, err := argTokens(arg)
	if err != nil {
		return "", err
	}

	e.Declare(constName, constDecl)
	e.Declare(spellName, spellDecl)

	values := opiter.WrapSeq(slices.Values(args)).
		Map(constLiteral).
		CollectSlice()

	return constName + "(" + v + ", " + strings.Join(values, ", ") + ")", nil
}

// constLiteral returns the Go literal kv spells: kv.Key quoted again when
// it was written quoted.
func constLiteral(kv dircmnt.KeyValue) string {
	if kv.KeyQuoted {
		return strconv.Quote(kv.Key)
	}

	return kv.Key
}

const constName = "kamaValidateConst"

func constDecl(s Spelling) (string, error) {
	v, err := s.Import(validationPath)
	if err != nil {
		return "", err
	}

	sl, err := s.Import("slices")
	if err != nil {
		return "", err
	}

	str, err := s.Import("strings")
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`func kamaValidateConst[T comparable](got T, want ...T) error {
	if !%[2]s.Contains(want, got) {
		spelled := make([]string, len(want))
		for i, w := range want {
			spelled[i] = kamaValidateSpell(w)
		}
		return %[1]s.New(%[1]s.ErrConst, "want one of %%s, got %%s",
			%[3]s.Join(spelled, ", "), kamaValidateSpell(got))
	}
	return nil
}`, v, sl, str), nil
}
