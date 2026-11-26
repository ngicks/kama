package std

import (
	"go/types"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
)

const sqlPath = "database/sql"

// sqlValues are the null types of database/sql and the field holding the
// value of each.
var sqlValues = map[string]string{
	"Null":        "V",
	"NullString":  "String",
	"NullInt64":   "Int64",
	"NullInt32":   "Int32",
	"NullInt16":   "Int16",
	"NullByte":    "Byte",
	"NullBool":    "Bool",
	"NullFloat64": "Float64",
	"NullTime":    "Time",
}

// Sql is the ext.TypeSupport of sql.Null[T] and the null types of
// database/sql, such as sql.NullString. It implements ext.Holder,
// ext.PresenceTester and ext.NullTester.
type Sql struct{}

// Name returns "database/sql".
func (Sql) Name() string { return sqlPath }

// Held tells the value a null type holds when Valid is set: V of
// sql.Null[T], String of sql.NullString and so on. The role of the value is
// "t".
func (Sql) Held(t types.Type, v ext.Gen) (string, types.Type, ext.Gen, ext.Gen, bool) {
	name, ok := sqlField(t)
	if !ok {
		return "", nil, nil, nil, false
	}

	return "t", field(t, name), lang.Expr("%s.Valid", v), lang.Expr("%s.%s", v, name), true
}

// Presence tells a null type holding a value by Valid.
func (Sql) Presence(t types.Type, v ext.Gen) (ext.Gen, string, bool) {
	if _, ok := sqlField(t); !ok {
		return nil, "", false
	}

	return lang.Expr("%s.Valid", v), "null", true
}

// Null tells a null type being null by Valid unset.
func (Sql) Null(t types.Type, v ext.Gen) (ext.Gen, bool) {
	if _, ok := sqlField(t); !ok {
		return nil, false
	}

	return lang.Expr("!%s.Valid", v), true
}

// sqlField returns the name of the field holding the value of t, a null
// type of database/sql, or false for another type.
func sqlField(t types.Type) (string, bool) {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != sqlPath {
		return "", false
	}

	name, ok := sqlValues[named.Obj().Name()]

	return name, ok
}
