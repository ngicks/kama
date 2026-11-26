package opt

import (
	"go/types"
	"strings"

	ftext "github.com/ngicks/kama/kamalib/generator/fieldtree/ext"
	"github.com/ngicks/kama/kamalib/generator/genext"
)

var _ ftext.Reacher = Support{}

// Reach tells how a template reaches the values the types of the support
// hold. The value of an opt.Option is v.Value, set only when v.IsSome, and
// that of an und.Und is v.Value, set only when v.IsDefined. An element of
// an elastic.Elastic is the opt.Option index v.Options i. The values of an
// either.Either are v.UnwrapLeft and v.UnwrapRight, those of an
// either.Result v.Value and v.Err, under the condition each names, and the
// fields of a tuple.TupleN are v.V1 to v.VN. Each adds to the key path
// what Part locates it by.
func (s Support) Reach(t types.Type, role, v string) (string, string, string, bool) {
	switch {
	case role != "t":
	case isOption(t):
		return v + ".Value", "set only when " + v + ".IsSome", "", true
	case isUnd(t):
		return v + ".Value", "set only when " + v + ".IsDefined; null when " + v + ".IsNull",
			"", true
	case isElastic(t):
		return "index " + v + ".Options i", "an opt.Option per element; range " + v +
			".Options", "[i]", true
	}

	if len(s.Roles(t)) == 0 {
		return "", "", "", false
	}

	_, _, _, segment, _, ok := s.Part(t, nil, role)
	if !ok {
		return "", "", "", false
	}

	var key string
	if segment == genext.SegmentField {
		key = "." + map[string]string{"l": "left", "r": "right", "ok": "ok", "err": "err"}[role]
	}

	switch role {
	case "l":
		return v + ".UnwrapLeft", "set only when " + v + ".IsLeft", key, true
	case "r":
		return v + ".UnwrapRight", "set only when " + v + ".IsRight", key, true
	case "ok":
		return v + ".Value", "set only when " + v + ".IsOk", key, true
	case "err":
		return v + ".Err", "set only when " + v + ".IsErr", key, true
	}

	field := "V" + strings.TrimPrefix(role, "v")

	return v + "." + field, "always set", "." + field, true
}
