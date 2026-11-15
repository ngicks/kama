// Package base holds what code generators share: the files they return and
// how those are staged, the names of generated files, the header and the
// stale file warnings, the placement of declarations several generated
// files share, helpers over loaded packages, and a workflow proc that a
// generator plugs into.
//
// A generator stays usable without the proc. It exposes a function that
// returns Files, and implements Generator to run as a workflow step.
package base
