package suitetest

import (
	"os"
	"testing"

	"gotest.tools/v3/assert"
)

func TestCompatible(t *testing.T) {
	for _, tc := range []struct {
		compatibility string
		want          bool
	}{
		{"", true},
		{"7", true},
		{"2020", true},
		{"2019", true},
		{"9999", false},
		{"<=2019", false},
		{"<=2020", true},
		{"6,<=2019", false},
		{"=2020", true},
		{"=2019", false},
		{"=3,7,<=2020", true},
		{"=3,7,<=2019", false},
		{"<=3,7", true},
		{"<=3", false},
		{"4,<=6,9999", false},
		{"2019,<=2020", true},
	} {
		got, err := compatible(tc.compatibility, Release)
		assert.NilError(t, err)
		assert.Equal(t, got, tc.want, "%q", tc.compatibility)
	}

	_, err := compatible("<=x", Release)
	assert.ErrorContains(t, err, `compatibility "<=x"`)
}

// TestGroups counts the groups and instances of the vendored suite that
// apply to draft 2020-12, so a refresh of the suite shows in a diff.
func TestGroups(t *testing.T) {
	groups := Groups(t, os.DirFS("../.."))

	var instances int

	for _, g := range groups {
		instances += len(g.Tests)
	}

	assert.Equal(t, len(groups), 493)
	assert.Equal(t, instances, 2288)
}

func TestRef(t *testing.T) {
	g := Group{File: "optional/a.json", Index: 3}

	assert.Equal(t, g.SchemaRef(), Dir+"/optional/a.json#/tests/3/schema")
	assert.Equal(
		t,
		g.ExternalSchemaRef("http://localhost:1234/a~b c.json"),
		Dir+"/optional/a.json#/tests/3/externalSchemas/http:~1~1localhost:1234~1a~0b%20c.json",
	)
}
