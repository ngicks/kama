// Package base holds what code generators share: the files they return and
// how those are staged, the names of generated files, the header and the
// stale file warnings, the placement of declarations several generated
// files share, and helpers over loaded packages.
//
// A generator exposes a function that returns Files, and wraps it in a
// workflow proc of its own, which reports the warnings and calls Stage.
package base
