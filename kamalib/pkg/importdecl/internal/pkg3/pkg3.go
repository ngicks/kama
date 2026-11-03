// Package pkg3 has only blank and dot imports.
package pkg3

import (
	_ "embed"
	. "strings" //nolint:staticcheck // the dot import is what this fixture tests
)

var _ = ToUpper
