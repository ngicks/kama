package jsonschemart

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/validation"
)

type failing struct{}

func (*failing) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if err := dec.SkipValue(); err != nil {
		return err
	}

	return validation.Field(validation.New(validation.ErrRequired, "inner"), "x")
}

func TestUnmarshalLocatesErrors(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		v       any
		pointer string
		text    string
	}{
		{
			"method error under array and object",
			`{"a":[{},{"b":{}}]}`,
			new(struct {
				A []struct {
					B und.Und[failing] `json:"b,omitzero"`
				} `json:"a"`
			}),
			"/a/1/b/x",
			"validation failed at .a[1].b.x: required: inner",
		},
		{
			"digits with a leading zero are a name",
			`{"07":null}`,
			new(map[string]string),
			"/07",
			"validation failed at .07: null: value is null",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Unmarshal(jsontext.Value(tc.in), tc.v)

			e, ok := errors.AsType[*validation.Error](err)
			assert.Assert(t, ok, "%T: %v", err, err)
			assert.Equal(t, e.Pointer(), tc.pointer)
			assert.Equal(t, err.Error(), tc.text)
		})
	}
}

// TestUnmarshalLocatesSemanticErrors avoids the message text of json: it words a semantic
// error with "cannot" or "unable to", chosen once per process.
func TestUnmarshalLocatesSemanticErrors(t *testing.T) {
	var v struct {
		A map[string]int64 `json:"a"`
	}

	err := Unmarshal(jsontext.Value(`{"a":{"7":"x"}}`), &v)

	e, ok := errors.AsType[*validation.Error](err)
	assert.Assert(t, ok, "%T: %v", err, err)
	assert.Equal(t, e.Pointer(), "/a/7")
	assert.Assert(t, strings.HasPrefix(err.Error(), "validation failed at .a[7]: "), err.Error())

	se, ok := errors.AsType[*json.SemanticError](err)
	assert.Assert(t, ok, "%T: %v", err, err)
	assert.Equal(t, se.JSONPointer, jsontext.Pointer(""))
	assert.Equal(t, se.ByteOffset, int64(0))
	assert.Equal(t, se.JSONKind, jsontext.Kind('"'))
	assert.Equal(t, se.GoType, reflect.TypeFor[int64]())
}

func TestUnmarshalRejectsNull(t *testing.T) {
	cases := []struct {
		name string
		v    any
	}{
		{"string", new(string)},
		{"int64", new(int64)},
		{"float64", new(float64)},
		{"bool", new(bool)},
		{"slice", new([]int64)},
		{"map", new(map[string]int64)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.ErrorIs(t, Unmarshal(jsontext.Value(`null`), tc.v), validation.ErrNull)
		})
	}

	var raw jsontext.Value
	assert.NilError(t, Unmarshal(jsontext.Value(`null`), &raw))
	assert.Equal(t, string(raw), `null`)

	var u und.Und[int64]
	assert.NilError(t, Unmarshal(jsontext.Value(`null`), &u))
	assert.Assert(t, u.IsNull())
}

func TestUnmarshalIntegerNumber(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"1", 1},
		{"1.0", 1},
		{"-0.0", 0},
		{"1e2", 100},
		{"1.5E+1", 15},
		{"0.001e3", 1},
		{"-12.000", -12},
		{"9007199254740993.0", 9007199254740993},
		{"9223372036854775807.0", 9223372036854775807},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			var v int64
			assert.NilError(t, Unmarshal(jsontext.Value(tc.in), &v))
			assert.Equal(t, v, tc.want)
		})
	}

	for _, in := range []string{"1.5", "1e-1", "0.5e0", "1e400", "9223372036854775808"} {
		t.Run(in, func(t *testing.T) {
			var v int64
			assert.ErrorIs(t, Unmarshal(jsontext.Value(in), &v), ErrType)
		})
	}

	var w struct {
		A und.Und[int64] `json:"a"`
		B []int64        `json:"b"`
	}
	assert.NilError(t, Unmarshal(jsontext.Value(`{"a":2.0,"b":[1.0,3e0]}`), &w))
	assert.Equal(t, w.A.Value(), int64(2))
	assert.DeepEqual(t, w.B, []int64{1, 3})

	err := Unmarshal(jsontext.Value(`{"a":2.5}`), &w)
	assert.ErrorIs(t, err, ErrType)
	assert.ErrorContains(t, err, ".a")
}

func TestUnmarshalObject(t *testing.T) {
	var m map[string]int64

	err := UnmarshalObject(jsontext.Value(`null`), &m)
	assert.ErrorIs(t, err, ErrType)
	assert.Equal(t, err.Error(), "validation failed: type: want object, got null")
	assert.NilError(t, UnmarshalObject(jsontext.Value(`{"a":1}`), &m))
}

func TestReadValueOutlivesDecoder(t *testing.T) {
	dec := jsontext.NewDecoder(strings.NewReader(`[{"a":1},{"b":2}]`))
	_, err := dec.ReadToken()
	assert.NilError(t, err)

	first, err := ReadValue(dec)
	assert.NilError(t, err)

	_, err = ReadValue(dec)
	assert.NilError(t, err)
	assert.Equal(t, string(first), `{"a":1}`)
}

func TestConversions(t *testing.T) {
	undefined, null, defined := und.Undefined[int64](), und.Null[int64](), und.Defined[int64](1)

	t.Run("Required", func(t *testing.T) {
		_, err := Required(undefined)
		assert.ErrorIs(t, err, validation.ErrRequired)
		_, err = Required(null)
		assert.ErrorIs(t, err, validation.ErrNull)

		v, err := Required(defined)
		assert.NilError(t, err)
		assert.Equal(t, v, int64(1))
	})
	t.Run("Optional", func(t *testing.T) {
		o, err := Optional(undefined)
		assert.NilError(t, err)
		assert.Assert(t, o.IsNone())

		_, err = Optional(null)
		assert.ErrorIs(t, err, validation.ErrNull)

		o, err = Optional(defined)
		assert.NilError(t, err)
		assert.Equal(t, o, opt.Some[int64](1))
	})
	t.Run("RequiredNullable", func(t *testing.T) {
		_, err := RequiredNullable(undefined)
		assert.ErrorIs(t, err, validation.ErrRequired)

		o, err := RequiredNullable(null)
		assert.NilError(t, err)
		assert.Assert(t, o.IsNone())

		o, err = RequiredNullable(defined)
		assert.NilError(t, err)
		assert.Equal(t, o, opt.Some[int64](1))
	})
	t.Run("RequiredPointer", func(t *testing.T) {
		_, err := RequiredPointer(undefined)
		assert.ErrorIs(t, err, validation.ErrRequired)

		p, err := RequiredPointer(defined)
		assert.NilError(t, err)
		assert.Equal(t, *p, int64(1))
	})
	t.Run("OptionalPointer", func(t *testing.T) {
		p, err := OptionalPointer(undefined)
		assert.NilError(t, err)
		assert.Assert(t, p == nil)

		_, err = OptionalPointer(null)
		assert.ErrorIs(t, err, validation.ErrNull)

		p, err = OptionalPointer(defined)
		assert.NilError(t, err)
		assert.Equal(t, *p, int64(1))
	})
	t.Run("RequiredNullablePointer", func(t *testing.T) {
		_, err := RequiredNullablePointer(undefined)
		assert.ErrorIs(t, err, validation.ErrRequired)

		p, err := RequiredNullablePointer(null)
		assert.NilError(t, err)
		assert.Assert(t, p == nil)

		p, err = RequiredNullablePointer(defined)
		assert.NilError(t, err)
		assert.Equal(t, *p, int64(1))
	})
	t.Run("OptionalNullablePointer", func(t *testing.T) {
		assert.Assert(t, OptionalNullablePointer(undefined) == nil)
		assert.Assert(t, OptionalNullablePointer(null) == nil)
		assert.Equal(t, *OptionalNullablePointer(defined), int64(1))
	})
	t.Run("DecodeRequired", func(t *testing.T) {
		_, err := DecodeRequired[jsontext.Value](nil)
		assert.ErrorIs(t, err, validation.ErrRequired)

		v, err := DecodeRequired[jsontext.Value](jsontext.Value(`null`))
		assert.NilError(t, err)
		assert.Equal(t, string(v), `null`)

		_, err = DecodeRequired[int64](jsontext.Value(`null`))
		assert.ErrorIs(t, err, validation.ErrNull)
	})
	t.Run("DecodeOptional", func(t *testing.T) {
		o, err := DecodeOptional[int64](nil)
		assert.NilError(t, err)
		assert.Assert(t, o.IsNone())

		o, err = DecodeOptional[int64](jsontext.Value(`2`))
		assert.NilError(t, err)
		assert.Equal(t, o, opt.Some[int64](2))

		_, err = DecodeOptional[int64](jsontext.Value(`"x"`))
		assert.Assert(t, err != nil)
	})
}

func TestEnum(t *testing.T) {
	assert.NilError(t, Enum("a", "a", "b"))

	err := Enum("c", "a", "b")
	assert.ErrorIs(t, err, ErrEnum)
	assert.Equal(t, err.Error(), "validation failed: enum: want one of [a b], got c")
}

func TestRejectUnknown(t *testing.T) {
	assert.NilError(t, RejectUnknown(nil))

	err := RejectUnknown(map[string]jsontext.Value{"b": nil, "a": nil})
	assert.DeepEqual(t, validation.Pointers(err), []string{"/a", "/b"})
	assert.ErrorIs(t, err, ErrUnknownMember)
	assert.Assert(t, strings.HasPrefix(err.Error(), "validation failed at .a:"), err.Error())
}

func TestSelection(t *testing.T) {
	obj := jsontext.Value(`{"kind":"cat","n":1}`)

	cases := []struct {
		name string
		got  bool
		want bool
	}{
		{"object", IsKind(obj, "object"), true},
		{"several types", IsKind(jsontext.Value(`true`), "null", "boolean"), true},
		{"integer accepts numbers", IsKind(jsontext.Value(`1.5`), "integer"), true},
		{"number is not string", IsKind(jsontext.Value(`1`), "string"), false},
		{"has members", HasMembers(obj, "kind", "n"), true},
		{"lacks member", HasMembers(obj, "kind", "m"), false},
		{"has members of non object", HasMembers(jsontext.Value(`[]`)), false},
		{"only members", OnlyMembers(obj, "kind", "n", "m"), true},
		{"extra member", OnlyMembers(obj, "kind"), false},
		{"member equals", MemberEquals(obj, "kind", `"cat"`), true},
		{"member equals canonically", MemberEquals(obj, "kind", `"cat"`), true},
		{"number equals canonically", MemberEquals(obj, "n", `1.0`), true},
		{"member differs", MemberEquals(obj, "kind", `"dog"`), false},
		{"member missing", MemberEquals(obj, "x", `"cat"`), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.got, tc.want)
		})
	}
}

func TestOneOfAnyOf(t *testing.T) {
	memberErr := MemberError("B", validation.Field(validation.New(validation.ErrNull, "x"), "f"))

	k, err := OneOf([]int{1}, []string{"A"}, []error{memberErr})
	assert.NilError(t, err)
	assert.Equal(t, k, 1)

	_, err = OneOf([]int{1, 2}, []string{"A", "C"}, nil)
	assert.ErrorIs(t, err, ErrMultipleMatch)
	assert.Equal(t, err.Error(), "validation failed: multiple matches: value matches A, C")

	_, err = OneOf[int](nil, nil, []error{memberErr})
	assert.ErrorIs(t, err, ErrNoMatch)
	assert.ErrorIs(t, err, validation.ErrNull)
	assert.DeepEqual(t, validation.Pointers(validation.Field(err, "u")), []string{"/u", "/u/f"})

	k, err = AnyOf([]int{1, 2}, nil)
	assert.NilError(t, err)
	assert.Equal(t, k, 1)

	_, err = AnyOf[int](nil, nil)
	assert.ErrorIs(t, err, ErrNoMatch)
}

func TestMarshalObjects(t *testing.T) {
	marshal := func(values ...any) (string, error) {
		var buf bytes.Buffer

		enc := jsontext.NewEncoder(&buf)
		if err := MarshalObjects(enc, values...); err != nil {
			return "", err
		}

		return strings.TrimSpace(buf.String()), nil
	}

	type a struct {
		X int `json:"x"`
		Y int `json:"y"`
	}

	type b struct {
		Y int `json:"y"`
		Z int `json:"z"`
	}

	out, err := marshal(a{X: 1, Y: 2}, b{Y: 3, Z: 4})
	assert.NilError(t, err)
	assert.Equal(t, out, `{"x":1,"y":2,"z":4}`)

	_, err = marshal(a{}, 5)
	assert.ErrorIs(t, err, ErrType)

	_, err = marshal(make(chan int))
	_, ok := errors.AsType[*json.SemanticError](err)
	assert.Assert(t, ok, "%v", err)
}

// TestImports keeps the runtime light for generated code: the standard library, the
// validation package and github.com/ngicks/option only.
func TestImports(t *testing.T) {
	allowed := []string{
		"github.com/ngicks/kama/kamalib/pkg/validation",
		"github.com/ngicks/option/",
	}

	entries, err := os.ReadDir(".")
	assert.NilError(t, err)

	fset := token.NewFileSet()
	parsed := 0

	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		assert.NilError(t, err)

		parsed++

		for _, spec := range f.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			assert.NilError(t, err)

			first, _, _ := strings.Cut(path, "/")

			ok := !strings.Contains(first, ".")
			for _, p := range allowed {
				ok = ok || strings.HasPrefix(path, p)
			}

			assert.Assert(t, ok, "%s imports %q", name, path)
		}
	}

	assert.Assert(t, parsed > 0)
}
