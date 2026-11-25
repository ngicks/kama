package oneof

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

func TestRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{
			"every member",
			`{"star":{"kind":"cat","lives":9},
			"pets":[{"kind":"dog","good":true},{"kind":"cat","lives":1}],
			"favorite":{"kind":"dog","good":false},"mixed":"s","amount":1.5}`,
		},
		{"integer kind", `{"star":{"kind":"cat","lives":9},"pets":[],"mixed":5}`},
		{"object kind", `{"star":{"kind":"dog","good":true},"pets":[],"mixed":{"note":"n"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v Zoo
			assert.NilError(t, json.Unmarshal([]byte(tc.in), &v))

			out, err := json.Marshal(v)
			assert.NilError(t, err)
			assert.Equal(t, canonical(t, out), canonical(t, []byte(tc.in)))
		})
	}
}

func TestSelectedMember(t *testing.T) {
	var v Zoo
	assert.NilError(t, json.Unmarshal([]byte(
		`{"star":{"kind":"cat","lives":9},"pets":[{"kind":"dog","good":true}],"mixed":{"note":"n"},
		"amount":1.5}`,
	), &v))

	assert.DeepEqual(t, v.Star.Kind, PetCat{Cat: Cat{Kind: "cat", Lives: 9}}, allFields)
	assert.DeepEqual(t, v.Pets[0].Kind, PetDog{Dog: Dog{Kind: "dog", Good: true}}, allFields)
	assert.DeepEqual(
		t,
		v.Mixed.Kind,
		ZooMixedObject{Object: ZooMixedObjectValue{Note: "n"}},
		allFields,
	)
	assert.DeepEqual(t, v.Amount.Value().Kind, ZooAmountNumber{Number: 1.5})
	assert.Assert(t, v.Favorite.IsNone())
}

func TestDecodeError(t *testing.T) {
	const pets = `"pets":[],"mixed":"s"`

	cases := []struct {
		name     string
		in       string
		pointers []string
		kind     error
	}{
		{
			"zero matches",
			`{"star":{"kind":"cow"},` + pets + `}`,
			[]string{"/star"},
			validator.ErrOneOf,
		},
		{
			"member error is kept",
			`{"star":{"kind":"cat","lives":"x"},` + pets + `}`,
			[]string{"/star", "/star/lives"},
			validator.ErrOneOf,
		},
		{
			"two matches",
			`{"star":{"kind":"cat","lives":9},` + pets + `,"amount":3}`,
			[]string{"/amount"},
			validator.ErrOneOf,
		},
		{
			"element of union slice",
			`{"star":{"kind":"cat","lives":9},"pets":[{"kind":"dog","good":true},{"kind":"dog"}],
			"mixed":"s"}`,
			[]string{"/pets/1"},
			validator.ErrOneOf,
		},
		{
			"null union",
			`{"star":{"kind":"cat","lives":9},"pets":[],"mixed":null}`,
			[]string{"/mixed"},
			validator.ErrNull,
		},
		{
			"kind excluded",
			`{"star":{"kind":"cat","lives":9},"pets":[],"mixed":true}`,
			[]string{"/mixed"},
			validator.ErrOneOf,
		},
		{
			"null optional union",
			`{"star":{"kind":"cat","lives":9},` + pets + `,"favorite":null}`,
			[]string{"/favorite"},
			validator.ErrNull,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := decodeError(t, tc.in)

			assert.DeepEqual(t, validator.Pointers(err), tc.pointers)
			assert.ErrorIs(t, err, tc.kind)
		})
	}
}

func TestMarshalNilKind(t *testing.T) {
	cases := []struct {
		name string
		v    Zoo
	}{
		{"field", Zoo{Mixed: ZooMixed{Kind: ZooMixedString{String: "s"}}}},
		{
			"slice element",
			Zoo{
				Star:  Pet{Kind: PetCat{Cat: Cat{Kind: "cat"}}},
				Pets:  []Pet{{}},
				Mixed: ZooMixed{Kind: ZooMixedString{String: "s"}},
			},
		},
		{
			"optional",
			Zoo{
				Star:     Pet{Kind: PetCat{Cat: Cat{Kind: "cat"}}},
				Favorite: opt.Some(Pet{}),
				Mixed:    ZooMixed{Kind: ZooMixedString{String: "s"}},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := json.Marshal(tc.v)
			assert.ErrorIs(t, err, validator.ErrNil)
		})
	}
}

func decodeError(t *testing.T, in string) error {
	t.Helper()

	var v Zoo

	err := json.Unmarshal([]byte(in), &v)
	assert.Assert(t, err != nil)

	se, ok := errors.AsType[*json.SemanticError](err)
	assert.Assert(t, ok, "%T: %v", err, err)

	return se.Err
}

func canonical(t *testing.T, b []byte) string {
	t.Helper()

	v := jsontext.Value(b)
	assert.NilError(t, v.Canonicalize())

	return string(v)
}
