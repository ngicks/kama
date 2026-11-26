package std

import (
	"go/types"

	ftext "github.com/ngicks/kama/kamalib/generator/fieldtree/ext"
	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

var (
	_ ftext.Reacher = Sql{}
	_ ftext.Reacher = Unique{}
)

// Reach tells the value of a null type: v.V of sql.Null[T], v.String of
// sql.NullString and so on, set only when v.Valid.
func (Sql) Reach(t types.Type, role, v string) (string, string, string, bool) {
	name, ok := sqlField(t)
	if !ok || role != "t" {
		return "", "", "", false
	}

	return v + "." + name, "set only when " + v + ".Valid", "", true
}

// Reach tells the value of a unique.Handle[T], v.Value, which the zero
// Handle does not hold.
func (Unique) Reach(t types.Type, role, v string) (string, string, string, bool) {
	if !typequery.IsNamed(t, uniquePath, "Handle") || role != "t" {
		return "", "", "", false
	}

	return v + ".Value", "set unless " + v + " is the zero Handle", "", true
}
