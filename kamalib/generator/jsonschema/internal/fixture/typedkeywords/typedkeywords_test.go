package typedkeywords

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"testing"

	"github.com/ngicks/option/opt"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/validation"
)

// check decodes each instance of ok into a T and each of bad into a T,
// which must fail with the error kind given.
func check[T any](t *testing.T, ok []string, bad map[string]error) {
	t.Helper()

	for _, text := range ok {
		var v T
		assert.NilError(t, json.Unmarshal([]byte(text), &v), text)
	}

	for text, kind := range bad {
		var v T
		assert.ErrorIs(t, json.Unmarshal([]byte(text), &v), kind, text)
	}
}

func TestOrder(t *testing.T) {
	check[Order](t,
		[]string{
			`{"kind": "cash", "id": 1}`,
			`{"kind": "card", "id": 1, "card": "c", "note": "n", "lines": [0, 1], "x-a": "s"}`,
		},
		map[string]error{
			`{"kind": "cash"}`:                                     validation.ErrRequired,
			`{"kind": "card", "id": 1}`:                            validation.ErrRequired,
			`{"kind": "cash", "id": 1, "card": "c"}`:               validation.ErrNot,
			`{"kind": "cash", "id": 1, "note": "x", "lines": [0]}`: validation.ErrNot,
			`{"kind": "cash", "id": 1, "note": "n"}`:               validation.ErrRequired,
			`{"kind": "cash", "id": 1, "lines": [1]}`:              validation.ErrContains,
			`{"kind": "cash", "id": 1, "x-a": 1}`:                  validation.ErrKind,
			`{"kind": "cash", "id": 1, "toolongname": 1}`:          validation.ErrLength,
		},
	)

	order := Order{
		Kind:  OrderKindCard,
		Card:  opt.Some("c"),
		Note:  opt.Some("n"),
		Lines: opt.Some([]int64{0, 2}),
		_x:    map[string]jsontext.Value{"id": jsontext.Value("1")},
	}
	assert.NilError(t, order.Validate(false))

	order.Card = opt.None[string]()
	assert.ErrorIs(t, order.Validate(false), validation.ErrRequired)

	order.Card = opt.Some("c")
	order.Lines = opt.Some([]int64{0, 0})
	err := order.Validate(false)
	assert.ErrorIs(t, err, validation.ErrContains)
	assert.Assert(t, len(validation.At(err, "/lines")) > 0, "%v", err)

	order.Lines = opt.None[[]int64]()
	order._x["x-b"] = jsontext.Value("2")
	err = order.Validate(false)
	assert.ErrorIs(t, err, validation.ErrKind)
	assert.Assert(t, len(validation.At(err, "/x-b")) > 0, "%v", err)

	delete(order._x, "id")
	delete(order._x, "x-b")
	assert.ErrorIs(t, order.Validate(false), validation.ErrRequired)
}

func TestShape(t *testing.T) {
	var v Shape
	assert.NilError(t, json.Unmarshal([]byte(`{"r": 1}`), &v))
	assert.NilError(t, v.Validate(false))

	circle, ok := v.Kind.(ShapeCircle)
	assert.Assert(t, ok, "%T", v.Kind)
	assert.Equal(t, circle.Circle.R, 1.0)

	// A union decoded alone reports a value no member takes on Validate.
	for _, text := range []string{`{}`, `{"r": 1, "side": 2}`, `{"r": 0}`} {
		var v Shape
		assert.NilError(t, json.Unmarshal([]byte(text), &v), text)
		assert.Assert(t, v.Validate(false) != nil, text)
	}

	assert.NilError(t, Shape{Kind: ShapeSquare{Square: Square{Side: 2}}}.Validate(false))
	assert.ErrorIs(
		t,
		Shape{Kind: ShapeCircle{Circle: Circle{R: 0}}}.Validate(false),
		validation.ErrNot,
	)
}

func TestPerson(t *testing.T) {
	check[Person](t,
		[]string{`{"name": "a", "age": 1}`, `{"name": "a"}`},
		map[string]error{`{"name": "a", "nick": "b"}`: validation.ErrNot},
	)

	p := Person{Named: Named{Name: "a"}, Aged: Aged{Age: opt.Some[int64](3)}}
	assert.NilError(t, p.Validate(false))

	p._x = map[string]jsontext.Value{"nick": jsontext.Value(`"b"`)}
	assert.ErrorIs(t, p.Validate(false), validation.ErrNot)
}

func TestBounded(t *testing.T) {
	check[Bounded](
		t,
		[]string{`{"lo": 1}`, `{"lo": 1, "hi": 2}`},
		map[string]error{
			`{"hi": 2}`:         validation.ErrRequired,
			`{"lo": 1, "x": 0}`: validation.ErrNot,
		},
	)

	assert.NilError(t, Bounded{Lo: opt.Some[int64](1)}.Validate(false))
	assert.ErrorIs(t, Bounded{Hi: opt.Some[int64](1)}.Validate(false), validation.ErrRequired)
}

func TestSealed(t *testing.T) {
	check[Sealed](t,
		[]string{`{}`, `{"a": 1, "x-b": [true]}`},
		map[string]error{`{"a": 1, "b": 2}`: validation.ErrOr},
	)

	s := Sealed{A: opt.Some[int64](1), _x: map[string]jsontext.Value{"b": jsontext.Value("2")}}
	err := s.Validate(false)
	assert.ErrorIs(t, err, validation.ErrOr)
	assert.Assert(t, len(validation.At(err, "/b")) > 0, "%v", err)
}

func TestTagged(t *testing.T) {
	check[Tagged](t,
		[]string{`{}`, `{"a": 1, "x-b": "s"}`},
		map[string]error{`{"a": 1, "b": 2}`: validation.ErrOr, `{"x-b": 1}`: validation.ErrKind},
	)

	v := Tagged{A: opt.Some[int64](1), _x: map[string]jsontext.Value{"x-b": jsontext.Value(`"s"`)}}
	assert.NilError(t, v.Validate(false))

	v._x["b"] = jsontext.Value("2")
	err := v.Validate(false)
	assert.ErrorIs(t, err, validation.ErrOr)
	assert.Assert(t, len(validation.At(err, "/b")) > 0, "%v", err)
}

func TestCounts(t *testing.T) {
	check[Order](t,
		[]string{`{"kind": "cash", "id": 1, "counts": {"a": 1}}`},
		map[string]error{},
	)

	var o Order
	assert.Assert(
		t,
		json.Unmarshal([]byte(`{"kind": "cash", "id": 1, "counts": {"a": "s"}}`), &o) != nil,
	)

	o.Counts = opt.Some(map[string]int64{"a": 1})
}
