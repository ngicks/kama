// Package base holds what kama generators share: the tool name, the Id
// of a generator and the registry and the markers keyed on it, the files
// they return and how those are staged, the names of generated files, the
// header and the stale file warnings, the placement of declarations
// several generated files share, the scope a generated file spells
// packages and types through, and helpers over loaded packages.
//
// A generator exposes a function that returns Files. A generator reading
// loaded packages runs it in a SourceProc. Another generator wraps it in a
// workflow proc of its own, which reports the warnings and calls Stage.
package base
