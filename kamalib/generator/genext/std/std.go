package std

import (
	"go/types"

	"github.com/ngicks/kama/kamalib/generator/genext"
	"github.com/ngicks/kama/kamalib/pkg/genhelper"
)

func init() { genext.RegisterTypeSupport(Sql{}, Unique{}, Time{}, Big{}, Netip{}) }

type (
	ref    = genhelper.Ref
	shadow = genhelper.Shadow
)

var lang genhelper.Lang[genext.Context]

// field returns the type of the field name of the struct type t, as the
// instance t is.
func field(t types.Type, name string) types.Type {
	obj, _, _ := types.LookupFieldOrMethod(t, false, nil, name)
	return obj.Type()
}
