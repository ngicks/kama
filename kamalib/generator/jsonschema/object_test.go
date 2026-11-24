package jsonschema

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestFallbackName(t *testing.T) {
	cases := []struct {
		name  string
		taken []string
		want  string
	}{
		{"free", []string{"Validate", "Name"}, "AdditionalProperties"},
		{"taken", []string{"AdditionalProperties"}, "AdditionalProperties_2"},
		{
			"first suffix taken",
			[]string{"AdditionalProperties", "AdditionalProperties_2"},
			"AdditionalProperties_3",
		},
		{
			"gap in the suffixes",
			[]string{"AdditionalProperties", "AdditionalProperties_3"},
			"AdditionalProperties_2",
		},
		{"suffix taken alone", []string{"AdditionalProperties_2"}, "AdditionalProperties"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, fallbackName("Root", tc.taken), tc.want)
		})
	}
}
