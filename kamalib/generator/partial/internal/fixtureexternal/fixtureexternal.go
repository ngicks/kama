// Package fixtureexternal generates partial types into its sub-packages, as
// spec.yaml describes. The sub-packages hold the models of the fixture
// package without directives, and partial.yaml gives their options. Their
// generated files match those of the fixture package.
package fixtureexternal

//go:generate go run ../generate -spec spec.yaml
