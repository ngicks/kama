package allof

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"reflect"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"github.com/ngicks/option/opt"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamaruntime/validator"
)

// allFields compares unexported fields too, such as those of opt.Option.
var allFields = gocmp.Exporter(func(reflect.Type) bool { return true })

func TestDecode(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Employee
	}{
		{
			"every member",
			`{"name":"a","age":3,"salary":1.5,"remote":true,"badge":"b","manager":{"name":"m"}}`,
			Employee{
				Person:   Person{Name: "a", Age: opt.Some[int64](3)},
				Contract: Contract{Salary: 1.5, Remote: opt.Some(true)},
				Badge:    "b",
				Manager:  opt.Some(Person{Name: "m"}),
			},
		},
		{
			"required only",
			`{"name":"a","salary":2,"badge":"b"}`,
			Employee{Person: Person{Name: "a"}, Contract: Contract{Salary: 2}, Badge: "b"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v Employee
			assert.NilError(t, json.Unmarshal([]byte(tc.in), &v))
			assert.DeepEqual(t, v, tc.want, allFields)

			out, err := json.Marshal(v)
			assert.NilError(t, err)
			assert.Equal(t, canonical(t, out), canonical(t, []byte(tc.in)))
		})
	}
}

func TestMarshalMergesMembers(t *testing.T) {
	out, err := json.Marshal(Employee{
		Person:   Person{Name: "a"},
		Contract: Contract{Salary: 1},
		Badge:    "b",
	})
	assert.NilError(t, err)
	assert.Equal(t, string(out), `{"name":"a","salary":1,"badge":"b"}`)
}

func TestDecodeError(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		pointers []string
		kind     error
	}{
		{"member", `{"name":"a","badge":"b"}`, []string{"/salary"}, validator.ErrRequired},
		{
			"members and own properties",
			`{"salary":1}`,
			[]string{"/badge", "/name"},
			validator.ErrRequired,
		},
		{
			"transparent allOf",
			`{"name":"a","salary":1,"badge":"b","manager":{}}`,
			[]string{"/manager/name"},
			validator.ErrRequired,
		},
		{
			"member null",
			`{"name":null,"salary":1,"badge":"b"}`,
			[]string{"/name"},
			validator.ErrNull,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v Employee

			err := json.Unmarshal([]byte(tc.in), &v)
			assert.Assert(t, err != nil)

			se, ok := errors.AsType[*json.SemanticError](err)
			assert.Assert(t, ok, "%T: %v", err, err)
			assert.DeepEqual(t, validator.Pointers(se.Err), tc.pointers)
			assert.ErrorIs(t, se.Err, tc.kind)
		})
	}
}

func canonical(t *testing.T, b []byte) string {
	t.Helper()

	v := jsontext.Value(b)
	assert.NilError(t, v.Canonicalize())

	return string(v)
}
