package hooks

import (
	"encoding/json/v2"
	"errors"
	"slices"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamaruntime/validator"
)

const doc = `{"id":"abc","lines":[{"qty":1},{"qty":2}],"sku":"AB"}`

// onceEach is the count of each function when every value of doc runs its
// functions once.
var onceEach = map[string]int{
	"validateOrderPre":  1,
	"validateOrderPost": 1,
	"validateLinePre":   2,
	"validateLinePost":  2,
	"validateSkuPre":    1,
	"validateSkuPost":   1,
}

// decode decodes b into an Order and returns the error that the
// json.SemanticError of json.Unmarshal wraps.
func decode(b string) (Order, error) {
	var o Order

	err := json.Unmarshal([]byte(b), &o)
	if se, ok := errors.AsType[*json.SemanticError](err); ok && se.Err != nil {
		return o, se.Err
	}

	return o, err
}

// reset clears the counts and makes the functions named in fail fail.
func reset(fail ...string) {
	clear(calls)
	clear(failing)

	for _, name := range fail {
		failing[name] = true
	}
}

// TestDecodeCallsOnce runs the functions once for each value on decoding,
// and once more for each value on a call of Validate.
func TestDecodeCallsOnce(t *testing.T) {
	reset()

	o, err := decode(doc)
	assert.NilError(t, err)
	assert.DeepEqual(t, calls, onceEach)

	reset()
	assert.NilError(t, o.Validate(false))
	assert.DeepEqual(t, calls, onceEach)

	reset()
	assert.NilError(t, o.Validate(true))
	assert.DeepEqual(t, calls, onceEach)
}

// TestDecodeKeepsRules runs the validators the generated directives name
// beside the functions of kama_validate.yaml. A nested value that fails
// stops the value holding it, so the functions of the holder do not run.
func TestDecodeKeepsRules(t *testing.T) {
	reset()

	_, err := decode(`{"id":"ab","lines":[],"sku":"A"}`)
	assert.DeepEqual(t, sortedPointers(err), []string{"/id"})
	assert.DeepEqual(t, calls, map[string]int{
		"validateOrderPre":  1,
		"validateOrderPost": 1,
		"validateSkuPre":    1,
		"validateSkuPost":   1,
	})

	reset()

	_, err = decode(`{"id":"ab","lines":[{"qty":0}],"sku":"a"}`)
	assert.DeepEqual(t, sortedPointers(err), []string{"/lines/0/qty"})
	assert.DeepEqual(t, calls, map[string]int{
		"validateLinePre":  1,
		"validateLinePost": 1,
	})
}

// TestDecodeFunctionErrors locates the errors of the functions at the value
// they validate, beside the problems decoding recorded.
func TestDecodeFunctionErrors(t *testing.T) {
	reset("validateOrderPost")

	_, err := decode(`{"id":5,"lines":[{"qty":1}]}`)
	assert.ErrorIs(t, err, validator.ErrKind)
	assert.ErrorContains(t, err, "validateOrderPost")
	assert.DeepEqual(t, sortedPointers(err), []string{"", "/id"})
	assert.Equal(t, calls["validateOrderPost"], 1)

	reset("validateLinePre")

	_, err = decode(`{"id":5,"lines":[{"qty":1}]}`)
	assert.ErrorContains(t, err, "validateLinePre")
	assert.DeepEqual(t, sortedPointers(err), []string{"/lines/0"})
	assert.Equal(t, calls["validateOrderPost"], 0)

	reset("validateOrderPre")

	err = Order{Id: "abc"}.Validate(true)
	assert.ErrorContains(t, err, "validateOrderPre")
	assert.DeepEqual(t, validator.Pointers(err), []string{""})
	assert.Equal(t, calls["validateOrderPost"], 1)
}

func sortedPointers(err error) []string {
	return slices.Sorted(slices.Values(validator.Pointers(err)))
}
