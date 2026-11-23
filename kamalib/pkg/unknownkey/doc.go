// Package unknownkey lists the keys a decoded table does not know.
//
// A type decoded with encoding/json/v2 captures the members that name no
// field in a field Unknown map[string]any `json:",embed"`. [Paths] turns
// such a map into key paths, and [Under] moves key paths below the key of
// a nested table, so that a type reports the unknown keys of itself and
// of its nested tables relative to itself.
package unknownkey
