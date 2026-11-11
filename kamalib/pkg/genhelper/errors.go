package genhelper

import "errors"

// ErrRedeclared reports a name a scope cannot declare again.
// [Scope.Shadow] returns it.
var ErrRedeclared = errors.New("genhelper: redeclared in this scope")
