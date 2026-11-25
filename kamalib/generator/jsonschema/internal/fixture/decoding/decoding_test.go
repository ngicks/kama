package decoding

import (
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sync/errgroup"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamaruntime/validator"
)

// valid holds a valid value of every required member of Root.
var valid = map[string]string{
	"s": `"x"`, "n": `1.5`, "i": `1`, "b": `true`, "arr": `["a"]`, "m": `{"k":1}`,
	"obj": `{"x":1,"y":1}`, "ns": `"x"`, "nn": `1.5`, "ni": `1`, "nb": `true`,
	"narr": `["a"]`, "nm": `{"k":1}`, "nobj": `{"x":1,"y":1}`,
}

// doc returns a document holding the members of valid overridden by
// members, a member whose value is empty left out.
func doc(members map[string]string) []byte {
	m := maps.Clone(valid)
	maps.Copy(m, members)

	var b strings.Builder

	b.WriteString("{")

	for _, k := range slices.Sorted(maps.Keys(m)) {
		if m[k] == "" {
			continue
		}

		if b.Len() > 1 {
			b.WriteString(",")
		}

		fmt.Fprintf(&b, "%q:%s", k, m[k])
	}

	b.WriteString("}")

	return []byte(b.String())
}

// decodeErr returns the error Unmarshal reports for b, unwrapped from the
// json.SemanticError of the root.
func decodeErr(b []byte, opts ...json.Options) error {
	var r Root

	err := json.Unmarshal(b, &r, opts...)
	if se, ok := errors.AsType[*json.SemanticError](err); ok && se.Err != nil {
		return se.Err
	}

	return err
}

// TestRequiredNullableMatrix decodes a member of each kind missing, null
// and set, for every combination of required and nullable.
func TestRequiredNullableMatrix(t *testing.T) {
	kinds := []struct{ name, value string }{
		{"s", `"x"`}, {"n", `1.5`}, {"i", `1`}, {"b", `true`},
		{"arr", `["a"]`}, {"m", `{"k":1}`}, {"obj", `{"x":1,"y":1}`},
	}

	for _, k := range kinds {
		for _, c := range []struct {
			prefix          string
			missing, isNull error
		}{
			{prefix: "", missing: validator.ErrRequired, isNull: validator.ErrNull},
			{prefix: "o", isNull: validator.ErrNull},
			{prefix: "n", missing: validator.ErrRequired},
			{prefix: "on"},
		} {
			name := c.prefix + k.name
			t.Run(name, func(t *testing.T) {
				for _, d := range []struct {
					value string
					want  error
				}{{"", c.missing}, {"null", c.isNull}, {k.value, nil}} {
					err := decodeErr(doc(map[string]string{name: d.value}))
					if d.want == nil {
						assert.NilError(t, err, "%s: %q", name, d.value)
						continue
					}

					assert.ErrorIs(t, err, d.want, "%s: %q", name, d.value)
					assert.DeepEqual(t, validator.Pointers(err), []string{"/" + name})
				}
			})
		}
	}
}

// TestNullElement rejects null as an element of a slice and as a value of
// a map, neither of which is nullable.
func TestNullElement(t *testing.T) {
	for _, c := range []struct {
		members map[string]string
		ptr     string
		is      error
	}{
		{map[string]string{"arr": `["a",null]`}, "/arr/1", validator.ErrNull},
		{map[string]string{"m": `{"k":null}`}, "/m/k", validator.ErrNull},
		{map[string]string{"items": `[{"x":1,"y":1},null]`}, "/items/1", validator.ErrKind},
		{map[string]string{"dict": `{"k":null}`}, "/dict/k", validator.ErrKind},
	} {
		err := decodeErr(doc(c.members))
		assert.ErrorIs(t, err, c.is, "%v", c.members)
		assert.DeepEqual(t, validator.Pointers(err), []string{c.ptr})
	}
}

// TestIntegerNumber decodes a number with a zero fraction into an int64
// and rejects any other number and one out of the range of int64.
func TestIntegerNumber(t *testing.T) {
	for _, c := range []struct {
		value string
		want  int64
	}{
		{"1.0", 1}, {"1e2", 100}, {"-0", 0}, {"-9223372036854775808", -9223372036854775808},
	} {
		var r Root
		assert.NilError(t, json.Unmarshal(doc(map[string]string{"i": c.value}), &r), c.value)
		assert.Equal(t, r.I, c.want, c.value)
	}

	for _, v := range []string{"1.5", "9223372036854775808", "1e19"} {
		err := decodeErr(doc(map[string]string{"i": v}))
		assert.ErrorIs(t, err, validator.ErrKind, v)
		assert.DeepEqual(t, validator.Pointers(err), []string{"/i"})
	}

	err := decodeErr(doc(map[string]string{"m": `{"k":2.5}`}))
	assert.ErrorIs(t, err, validator.ErrKind)
	assert.DeepEqual(t, validator.Pointers(err), []string{"/m/k"})
}

// TestLocationLegacy locates an error below the root under the options of
// json v1 and through json v1 itself as json/v2 does.
func TestLocationLegacy(t *testing.T) {
	cases := []struct {
		members map[string]string
		ptr     string
	}{
		{map[string]string{"items": `[{"x":1,"y":1},{"x":-1,"y":2}]`}, "/items/1/x"},
		{map[string]string{"dict": `{"k":{"x":-1,"y":0}}`}, "/dict/k/x"},
		{map[string]string{"arr": `["a",null]`}, "/arr/1"},
		{map[string]string{"items": `[{"x":1,"y":1},{"x":"bad","y":2}]`}, "/items/1/x"},
		{map[string]string{"all": `{"p":{"x":-1,"y":0}}`}, "/all/p/x"},
	}

	for _, c := range cases {
		b := doc(c.members)

		err := decodeErr(b)
		assert.DeepEqual(t, validator.Pointers(err), []string{c.ptr})

		err = decodeErr(b, jsonv1.DefaultOptionsV1())
		assert.DeepEqual(t, validator.Pointers(err), []string{c.ptr})

		var r Root

		err = jsonv1.Unmarshal(b, &r)
		_, ok := errors.AsType[*validator.Error](err)
		assert.Assert(t, ok, "%T: %v", err, err)
		assert.DeepEqual(t, validator.Pointers(err), []string{c.ptr})
	}
}

// TestCallerUnmarshalers runs an unmarshaler the caller passes for every
// value of its type, at every depth, in a union member and in a member of
// allOf too.
func TestCallerUnmarshalers(t *testing.T) {
	var points int

	opts := json.WithUnmarshalers(json.UnmarshalFromFunc(
		func(dec *jsontext.Decoder, p *Point) error {
			points++

			if _, err := dec.ReadValue(); err != nil {
				return err
			}

			*p = Point{X: 7}

			return nil
		},
	))

	var r Root

	assert.NilError(t, json.Unmarshal(doc(map[string]string{
		"items": `[{"x":1,"y":1},{"x":2,"y":1}]`,
		"dict":  `{"k":{"x":1,"y":1}}`,
		"u":     `{"x":1,"y":1}`,
		"all":   `{"p":{"x":1,"y":1}}`,
	}), &r, opts))

	// obj, nobj, two items, one value of dict, u and p of all.
	assert.Equal(t, points, 7)
	assert.Equal(t, r.Obj.X, int64(7))
	assert.Equal(t, r.Items.Value()[1].X, int64(7))
	assert.Equal(t, r.Dict.Value()["k"].X, int64(7))
	assert.Equal(t, r.All.Value().Option1.P.Value().X, int64(7))

	strs := json.WithUnmarshalers(json.UnmarshalFromFunc(
		func(dec *jsontext.Decoder, s *string) error {
			if _, err := dec.ReadValue(); err != nil {
				return err
			}

			*s = "caller"

			return nil
		},
	))

	r = Root{}
	assert.NilError(t, json.Unmarshal(doc(map[string]string{"arr": `["a",1]`}), &r, strs))
	assert.DeepEqual(t, r.Arr, []string{"caller", "caller"})
	assert.Equal(t, r.S, "caller")

	// A null the schema rejects is recorded before an unmarshaler sees it.
	err := decodeErr(doc(map[string]string{"arr": `["a",null]`}), strs)
	assert.ErrorIs(t, err, validator.ErrNull)
	assert.DeepEqual(t, validator.Pointers(err), []string{"/arr/1"})
}

// TestReportsEveryNull reports every null in a position that rejects it
// together with the other errors of the conversion, and leaves the decoder
// at the next value.
func TestReportsEveryNull(t *testing.T) {
	b := doc(map[string]string{
		"s":      "",
		"i":      `1.5`,
		"arr":    `["a",null]`,
		"m":      `{"k":null,"j":1}`,
		"oarr":   `null`,
		"onarr":  `[null]`,
		"stamps": `{"k":null}`,
		"narr":   `null`,
	})

	err := decodeErr(b)
	assert.DeepEqual(t, sortedPointers(err), []string{
		"/arr/1", "/i", "/m/k", "/oarr", "/onarr/0", "/s", "/stamps/k",
	})
	assert.ErrorIs(t, err, validator.ErrNull)
	assert.ErrorIs(t, err, validator.ErrRequired)
	assert.ErrorIs(t, err, validator.ErrKind)

	dec := jsontext.NewDecoder(strings.NewReader(string(b) + string(doc(nil))))

	var r Root

	assert.Assert(t, json.UnmarshalDecode(dec, &r) != nil)
	assert.NilError(t, json.UnmarshalDecode(dec, &r))
}

func sortedPointers(err error) []string {
	return slices.Sorted(slices.Values(validator.Pointers(err)))
}

// TestCallerOptionsConcurrent decodes and encodes one document at once
// from two goroutines, each with its own unmarshalers and marshalers of
// string and Point, and finds each call's own at every depth: in a member,
// a slice, a map, a union member and a member of allOf.
func TestCallerOptionsConcurrent(t *testing.T) {
	b := doc(map[string]string{
		"when":   `"2024-01-01T00:00:00Z"`,
		"stamps": `{"k":"2024-01-01T00:00:00Z"}`,
		"oarr":   `["a"]`,
		"items":  `[{"x":1,"y":1}]`,
		"dict":   `{"k":{"x":1,"y":1}}`,
		"u":      `{"x":1,"y":1}`,
		"all":    `{"p":{"x":1,"y":1},"q":"z"}`,
		"twice":  `"t"`,
	})

	var g errgroup.Group

	for _, c := range []struct {
		tag string
		x   int64
	}{{"A", 1}, {"B", 2}} {
		g.Go(func() error {
			for range 200 {
				if err := roundTrip(b, c.tag, c.x); err != nil {
					return fmt.Errorf("%s: %w", c.tag, err)
				}
			}

			return nil
		})
	}

	assert.NilError(t, g.Wait())
}

// roundTrip decodes b with unmarshalers setting every string to tag and
// every Point to one of X x, then encodes the value with marshalers writing
// "encoded " and tag, and X x+10, and reports a value either did not
// reach.
func roundTrip(b []byte, tag string, x int64) error {
	unmarshalers := json.WithUnmarshalers(json.JoinUnmarshalers(
		json.UnmarshalFromFunc(func(dec *jsontext.Decoder, s *string) error {
			*s = tag
			return dec.SkipValue()
		}),
		json.UnmarshalFromFunc(func(dec *jsontext.Decoder, p *Point) error {
			*p = Point{X: x}
			return dec.SkipValue()
		}),
	))

	var r Root
	if err := json.Unmarshal(b, &r, unmarshalers); err != nil {
		return err
	}

	all := r.All.Value()
	strs := []string{
		r.S, r.When.Value(), only[string](r.Stamps.Value()), r.Arr[0], r.Oarr.Value()[0],
		all.Option2.Q.Value(), r.Twice.Value().Kind.(RootTwiceString).String,
	}
	points := []Point{
		r.Obj, r.Items.Value()[0], only[Point](r.Dict.Value()), r.U.Value().Kind.(RootUPoint).Point,
		all.Option1.P.Value(),
	}

	if slices.ContainsFunc(strs, func(s string) bool { return s != tag }) ||
		slices.ContainsFunc(points, func(p Point) bool { return p.X != x }) {
		return fmt.Errorf("decoded %v and %v", strs, points)
	}

	marshalers := json.WithMarshalers(json.JoinMarshalers(
		json.MarshalToFunc(func(enc *jsontext.Encoder, s string) error {
			return enc.WriteToken(jsontext.String("encoded " + tag))
		}),
		json.MarshalToFunc(func(enc *jsontext.Encoder, p Point) error {
			return enc.WriteValue(jsontext.Value(fmt.Sprintf(`{"x":%d}`, x+10)))
		}),
	))

	out, err := json.Marshal(r, marshalers)
	if err != nil {
		return err
	}

	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		return err
	}

	got := []any{
		at(m, "s"),
		at(m, "when"),
		only[any](at(m, "stamps")),
		at(m, "arr", 0),
		at(m, "oarr", 0),
		at(m, "all", "q"),
		at(m, "twice"),
		at(
			m,
			"obj",
			"x",
		),
		at(m, "items", 0, "x"),
		at(only[any](at(m, "dict")), "x"),
		at(m, "u", "x"),
		at(m, "all", "p", "x"),
	}
	want := slices.Repeat([]any{"encoded " + tag}, 7)
	want = append(want, slices.Repeat([]any{float64(x + 10)}, 5)...)

	if !slices.Equal(got, want) {
		return fmt.Errorf("encoded %s", out)
	}

	return nil
}

// only returns the value of m, a map of one entry, whose key the
// unmarshalers and marshalers of string rewrite. A value other than a map
// gives the zero value.
func only[V any](m any) V {
	var v V

	mm, _ := m.(map[string]V)
	for _, v = range mm {
	}

	return v
}

// at returns the value of v at path, a member name or an index per step.
func at(v any, path ...any) any {
	for _, p := range path {
		switch p := p.(type) {
		case string:
			m, _ := v.(map[string]any)
			v = m[p]
		case int:
			s, _ := v.([]any)
			if p >= len(s) {
				return nil
			}

			v = s[p]
		}
	}

	return v
}

// TestCallerDuplicates decodes an object holding a member twice as the
// options of the caller tell, alike in a plain member, a union member and
// a member of allOf.
func TestCallerDuplicates(t *testing.T) {
	for _, members := range []map[string]string{
		{"obj": `{"x":1,"x":2,"y":1}`},
		{"u": `{"x":1,"x":2,"y":1}`},
		{"all": `{"q":"a","q":"b"}`},
	} {
		b := doc(members)

		var r Root

		err := json.Unmarshal(b, &r)
		_, ok := errors.AsType[*jsontext.SyntacticError](err)
		assert.Assert(t, ok, "%v: %T: %v", members, err, err)

		r = Root{}
		assert.NilError(t, json.Unmarshal(b, &r, jsontext.AllowDuplicateNames(true)), "%v", members)
		assert.NilError(t, r.Validate(false), "%v", members)
	}
}

// TestExactNames matches member names exactly whatever the caller passes,
// alike in a plain member and a union member.
func TestExactNames(t *testing.T) {
	for _, opts := range []json.Options{
		json.MatchCaseInsensitiveNames(true),
		jsonv1.DefaultOptionsV1(),
	} {
		err := decodeErr(doc(map[string]string{"obj": `{"X":1,"y":1}`}), opts)
		assert.ErrorIs(t, err, validator.ErrRequired)
		assert.DeepEqual(t, validator.Pointers(err), []string{"/obj/x"})

		err = decodeErr(doc(map[string]string{"u": `{"X":1,"y":1}`}), opts)
		assert.ErrorIs(t, err, validator.ErrOneOf)
		assert.DeepEqual(t, validator.Pointers(err), []string{"/u"})
	}
}

// TestRequiredTwice selects a union member whose required names one member
// twice for an object holding it once.
func TestRequiredTwice(t *testing.T) {
	var r Root

	assert.NilError(t, json.Unmarshal(doc(map[string]string{"twice": `{"x":1}`}), &r))
	assert.NilError(t, r.Validate(false))

	_, ok := r.Twice.Value().Kind.(RootTwiceObject)
	assert.Assert(t, ok, "%T", r.Twice.Value().Kind)
}
