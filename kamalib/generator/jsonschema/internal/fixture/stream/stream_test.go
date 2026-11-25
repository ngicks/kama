package stream

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamaruntime/validator"
)

// TestDecodeStreams decodes a document of a million elements that a reader
// produces as it is read. The element bad of a document fails, which stops
// the decoding there, as encoding/json stops a slice at an element that
// fails, and leaves the value alone.
func TestDecodeStreams(t *testing.T) {
	const n = 1_000_000

	r := &items{n: n, bad: func(i int) bool { return i == 1000 }}

	v := Stream{{N: 7}}

	err := json.UnmarshalRead(r, &v)
	assert.Assert(t, err != nil)

	se, ok := errors.AsType[*json.SemanticError](err)
	assert.Assert(t, ok, "%T: %v", err, err)
	assert.DeepEqual(t, validator.Pointers(se.Err), []string{"/1000/n"})
	assert.ErrorIs(t, se.Err, validator.ErrCompare)
	assert.Assert(t, r.read < r.size(), "%d of %d", r.read, r.size())
	assert.DeepEqual(t, v, Stream{{N: 7}})
}

// TestDecodeStreamsValid decodes a valid document of a million elements
// from the same reader.
func TestDecodeStreamsValid(t *testing.T) {
	const n = 1_000_000

	r := &items{n: n, bad: func(int) bool { return false }}

	var v Stream

	assert.NilError(t, json.UnmarshalRead(r, &v))
	assert.Equal(t, len(v), n)
	assert.Equal(t, v[n-1].N, int64(n-1))
	assert.Equal(t, r.read, r.size())
}

// items reads as the JSON array of n objects {"n":i}, an element bad
// reports holding -1 instead, producing the text as it is read. read
// counts the bytes read.
type items struct {
	n    int
	bad  func(i int) bool
	next int
	buf  []byte
	read int
}

func (r *items) Read(p []byte) (int, error) {
	for len(r.buf) == 0 {
		if r.next > r.n {
			return 0, io.EOF
		}

		r.buf = r.element(r.next)
		r.next++
	}

	c := copy(p, r.buf)
	r.buf = r.buf[c:]
	r.read += c

	return c, nil
}

// element returns the text of element i, the opening bracket before the
// first and the closing bracket for i equal to n.
func (r *items) element(i int) []byte {
	if i == r.n {
		return []byte("]")
	}

	sep := ","
	if i == 0 {
		sep = "["
	}

	if r.bad(i) {
		return []byte(sep + `{"n":-1}`)
	}

	return fmt.Appendf(nil, `%s{"n":%d}`, sep, i)
}

// size returns the length of the whole document.
func (r *items) size() int {
	size := 0
	for i := 0; i <= r.n; i++ {
		size += len(r.element(i))
	}

	return size
}
