// Package fixturefile generates Validate methods into its sub-packages, as
// spec.yaml describes. The sub-packages hold types of the fixture package
// without directives. validate.yaml and the package option files give
// their options, and the generated files match those of the fixture
// package.
package fixturefile

//go:generate go run ../../../../cmd/kama-simple --spec spec.yaml
