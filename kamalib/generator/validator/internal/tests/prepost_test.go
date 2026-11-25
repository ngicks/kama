package tests

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/internal/fixture/model"
	"github.com/ngicks/kama/kamaruntime/validator"
)

func TestPrePostStruct(t *testing.T) {
	cases := []struct {
		name     string
		stop     bool
		n        int
		fail     model.Fail
		wantLog  []string
		wantMsgs []string
		pointers []string
	}{
		{
			name:    "valid",
			stop:    true,
			n:       1,
			wantLog: []string{"pre 1 true", "post 1 true"},
		},
		{
			name:     "pre fails and stops",
			stop:     true,
			fail:     model.Fail{Pre: true},
			wantLog:  []string{"pre 0 true", "post 0 true"},
			wantMsgs: []string{"pre 0 true"},
			pointers: []string{""},
		},
		{
			name:     "rule fails and stops",
			stop:     true,
			wantLog:  []string{"pre 0 true", "post 0 true"},
			pointers: []string{"/n"},
		},
		{
			name:     "post fails after a rule stops",
			stop:     true,
			fail:     model.Fail{Post: true},
			wantLog:  []string{"pre 0 true", "post 0 true"},
			wantMsgs: []string{"post 0 true"},
			pointers: []string{"", "/n"},
		},
		{
			name:     "every one fails",
			fail:     model.Fail{Pre: true, Post: true},
			wantLog:  []string{"pre 0 false", "post 0 false"},
			wantMsgs: []string{"pre 0 false", "post 0 false"},
			pointers: []string{"", "/n"},
		},
		{
			name:     "post fails",
			n:        1,
			fail:     model.Fail{Post: true},
			wantLog:  []string{"pre 1 false", "post 1 false"},
			wantMsgs: []string{"post 1 false"},
			pointers: []string{""},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var log []string

			err := model.Hooked{N: tc.n, Fail: tc.fail, Log: &log}.Validate(tc.stop)
			assert.DeepEqual(t, log, tc.wantLog)
			assert.DeepEqual(t, validator.Pointers(err), tc.pointers)

			for _, msg := range tc.wantMsgs {
				assert.ErrorContains(t, err, msg)
			}
		})
	}
}

// TestPrePostNested calls the functions once for each value, the nested
// one from the nested Validate.
func TestPrePostNested(t *testing.T) {
	var log []string

	v := model.Hooked{N: 1, Log: &log, Next: &model.Hooked{N: 2, Log: &log}}
	assert.NilError(t, v.Validate(false))
	assert.DeepEqual(t, log, []string{"pre 1 false", "pre 2 false", "post 2 false", "post 1 false"})
}

func TestPrePostNamedType(t *testing.T) {
	cases := []struct {
		name     string
		stop     bool
		v        model.Tally
		want     model.Tally
		wantMsgs []string
		// notMsgs are the errors a stop skips.
		notMsgs []string
	}{
		{
			name: "valid",
			v:    model.Tally{"pre": 0, "post": 0},
			want: model.Tally{"pre": 1, "post": 1},
		},
		{
			name:     "pre fails and stops",
			stop:     true,
			v:        model.Tally{"pre": 0, "post": 0, "failPre": 0, "x": 0, "y": 0},
			want:     model.Tally{"pre": 1, "post": 1, "failPre": 0, "x": 0, "y": 0},
			wantMsgs: []string{"pre"},
			notMsgs:  []string{"length"},
		},
		{
			name:     "rule fails and stops",
			stop:     true,
			v:        model.Tally{"pre": 0, "post": 0, "x": 0, "y": 0, "z": 0},
			want:     model.Tally{"pre": 1, "post": 1, "x": 0, "y": 0, "z": 0},
			wantMsgs: []string{"length"},
		},
		{
			name:     "every one fails",
			v:        model.Tally{"pre": 0, "post": 0, "failPre": 0, "failPost": 0, "x": 0},
			want:     model.Tally{"pre": 1, "post": 1, "failPre": 0, "failPost": 0, "x": 0},
			wantMsgs: []string{"pre", "length", "post"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.v.Validate(tc.stop)
			assert.DeepEqual(t, tc.v, tc.want)

			if len(tc.wantMsgs) == 0 {
				assert.NilError(t, err)
			}

			for _, msg := range tc.wantMsgs {
				assert.ErrorContains(t, err, msg)
			}

			for _, msg := range tc.notMsgs {
				assert.Assert(t, !strings.Contains(err.Error(), msg), err)
			}
		})
	}
}
