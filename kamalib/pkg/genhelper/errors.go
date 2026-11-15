package genhelper

import "errors"

// ErrRedeclared reports a name a scope cannot declare again.
// [Scope.Shadow] returns it.
var ErrRedeclared = errors.New("genhelper: redeclared in this scope")

// ErrUndeclared reports a role that neither the scope nor a scope enclosing
// it declares. [Expr] and [Stmt] return it for a [Ref] to such a role.
var ErrUndeclared = errors.New("genhelper: undeclared in this scope")
