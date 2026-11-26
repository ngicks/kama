// Package fixtureext generates Validate methods into its sub-packages, as
// spec.yaml describes. The types of package textext that hold other values
// have no type support in Go: ext.toml describes them.
package fixtureext

//go:generate go run ../../../../cmd/kama-simple --spec spec.yaml
