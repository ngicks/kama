package validator

import (
	"fmt"
	"go/types"
	"reflect"
	"slices"
	"strings"

	"github.com/ngicks/kama/kamalib/generator/validator/rule"
)

// structView is the rule.Struct of the struct type st, whose value x
// denotes in generated code of the package pkg. It reads the fields from
// st at every call.
type structView struct {
	st  *types.Struct
	x   genExpr
	pkg *types.Package
}

var _ rule.Struct = structView{}

func (s structView) Members() ([]string, error) {
	var names []string

	for i := range s.st.NumFields() {
		if inlines(s.st, i) {
			return nil, fmt.Errorf(
				"field %s inlines the members of an embedded struct, which are not listed",
				s.st.Field(i).Name(),
			)
		}

		if name, ok := memberName(s.st, i); ok {
			names = append(names, name)
		}
	}

	return names, nil
}

func (s structView) Member(name string) (types.Type, genExpr, bool) {
	for i := range s.st.NumFields() {
		if n, ok := memberName(s.st, i); ok && n == name {
			v := s.st.Field(i)
			return v.Type(), lang.Expr("%s.%s", s.x, v.Name()), true
		}
	}

	return nil, nil, false
}

func (s structView) Fallback() (types.Type, genExpr, bool) {
	for i := range s.st.NumFields() {
		v := s.st.Field(i)
		if tagOption(s.st, i, "embed") && !isStructOrPointer(v.Type()) && accessible(v, s.pkg) {
			return v.Type(), lang.Expr("%s.%s", s.x, v.Name()), true
		}
	}

	return nil, nil, false
}

// memberName returns the name of the JSON member the field i of st encodes
// as, or false when it encodes as none: it is unexported, its json tag is
// "-", or encoders inline it or use it as the embedded fallback.
func memberName(st *types.Struct, i int) (string, bool) {
	v := st.Field(i)
	name, omitted := tagName(st, i)

	if omitted || !v.Exported() || inlines(st, i) || tagOption(st, i, "embed") {
		return "", false
	}

	if name == "" {
		name = v.Name()
	}

	return name, true
}

// tagName returns the name the json tag of the field i of st gives it,
// empty for none, and whether the tag is "-", which leaves the field out.
func tagName(st *types.Struct, i int) (string, bool) {
	tag := reflect.StructTag(st.Tag(i)).Get("json")
	if tag == "-" {
		return "", true
	}

	name, _, _ := strings.Cut(tag, ",")

	return name, false
}

// tagOption reports whether the json tag of the field i of st holds the
// option opt.
func tagOption(st *types.Struct, i int, opt string) bool {
	tag := reflect.StructTag(st.Tag(i)).Get("json")
	_, opts, _ := strings.Cut(tag, ",")

	return slices.Contains(strings.Split(opts, ","), opt)
}

// inlines reports whether encoders write the fields of the field i of st
// as members of st: the field is an embedded struct, or pointer to one,
// without a json name, or a struct tagged json:",embed".
func inlines(st *types.Struct, i int) bool {
	v := st.Field(i)
	if !isStructOrPointer(v.Type()) {
		return false
	}

	name, omitted := tagName(st, i)

	return !omitted && (name == "" && v.Embedded() || tagOption(st, i, "embed"))
}

// isStructOrPointer reports whether t is a struct type or a pointer to one.
func isStructOrPointer(t types.Type) bool {
	if p, ok := types.Unalias(t).(*types.Pointer); ok {
		t = p.Elem()
	}

	_, ok := t.Underlying().(*types.Struct)

	return ok
}
