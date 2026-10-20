package workflow

import (
	"context"
	"errors"
	"testing"

	"gotest.tools/v3/assert"
)

func testRegistry(seen *[]double) Registry {
	return Registry{
		"fs": func(map[string]any) (Node, error) { return targetNode(), nil },
		"source": func(param map[string]any) (Node, error) {
			v, ok := param["value"].(int)
			if !ok {
				return nil, errors.New("value must be an int")
			}

			return source(v), nil
		},
		"double":   func(map[string]any) (Node, error) { return doubler(), nil },
		"consumer": func(map[string]any) (Node, error) { return consumer(seen), nil },
	}
}

func TestSpecBuild(t *testing.T) {
	var seen []double
	spec := Spec{
		Version: SpecVersion,
		Nodes: []NodeSpec{
			{Name: "fs", Kind: "fs"},
			{Name: "src", Kind: "source", Deps: []string{"fs"}, Param: map[string]any{"value": 3}},
			{Name: "dbl", Kind: "double", Deps: []string{"src"}},
			{Name: "c", Kind: "consumer", Deps: []string{"dbl"}},
		},
	}

	g, err := spec.Build(testRegistry(&seen))
	assert.NilError(t, err)

	assert.DeepEqual(t, g.Names(), []string{"fs", "src", "dbl", "c"})

	assert.NilError(t, g.Run(context.Background()))
	assert.DeepEqual(t, seen, []double{6})
}

func TestSpecBuildErrors(t *testing.T) {
	var seen []double
	reg := testRegistry(&seen)

	_, err := Spec{Version: 2}.Build(reg)
	assert.ErrorIs(t, err, ErrUnsupportedVersion)

	_, err = Spec{Version: SpecVersion, Nodes: []NodeSpec{{Name: "x", Kind: "nope"}}}.Build(reg)
	assert.ErrorIs(t, err, ErrUnknownKind)

	_, err = Spec{Version: SpecVersion, Nodes: []NodeSpec{
		{Name: "fs", Kind: "fs"},
		{Name: "src", Kind: "source", Deps: []string{"fs"}, Param: map[string]any{"value": "3"}},
	}}.Build(reg)
	assert.ErrorContains(t, err, `node "src": value must be an int`)

	_, err = Spec{Version: SpecVersion, Nodes: []NodeSpec{
		{Name: "c", Kind: "consumer"},
	}}.Build(reg)
	assert.ErrorIs(t, err, ErrNoProvider)
}
