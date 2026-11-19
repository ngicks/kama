// Package fixturepackage generates partial types into its sub-packages, as
// spec.yaml describes. The sub-packages hold the models of the fixture
// package without directives, and package option files give their options:
// model/kama_partial.yaml is found by its default name, and
// options/pkgmark.yaml is named by the directive of pkgmark. Their
// generated files match those of the fixture package.
package fixturepackage

//go:generate go run ../generate -spec spec.yaml
