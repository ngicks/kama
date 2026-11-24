// Package fixturetest generates partial types for targets declared in test
// files of its sub-packages, as spec.yaml describes.
//
// The tests of model call the generated types, so model does not load
// without the generated files. Regenerate over them rather than deleting
// them first.
package fixturetest

//go:generate go run ../../../../cmd/kama-simple --spec spec.yaml
