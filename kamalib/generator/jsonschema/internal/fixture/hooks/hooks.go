// Package hooks holds the types spec.yaml generates from schema.json, and
// the pre and post functions kama_validate.yaml gives them.
//
//kama:option-file jsonschema=schema.json
package hooks

//go:generate go run ../../../../../cmd/kama-simple --spec spec.yaml
