package genhelper

import "errors"

// ErrRedeclared reports a name a scope cannot declare again.
// [Scope.Shadow] returns it.
var ErrRedeclared = errors.New("genhelper: redeclared in this scope")

// ErrNotPartial reports text that does not parse as the partial text asked
// for: one expression, the statements of a function body or top-level
// declarations other than imports. The Parse functions return it for text
// escaping the file they wrap it in, such as statements closing the
// function body.
var ErrNotPartial = errors.New("genhelper: text is not the partial text asked for")

// ErrNoFileSet reports an ast function or step without a token.FileSet.
// The ast Parse and Print functions return it for a nil file set.
// [AstExpr], [AstStmts] and [AstDecls] return it when [Context.Package] or
// its Fset is nil.
var ErrNoFileSet = errors.New("genhelper: no file set")
