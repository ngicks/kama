// Package genhelper renders Go source text through a chain of steps.
//
// A [Step] receives the text the steps before it produced and appends to
// it, wraps it or replaces it. [Steps] runs steps in order and is a step
// itself, so chains nest. The text is partial: an expression, statements or
// declarations. [File] assembles declarations into a whole file.
//
// Steps share a [Context]. Its [Scope] hands out identifiers by role, so
// independently written steps declare locals without duplicating or
// shadowing names. Its [Dest] spells package qualifiers and types for the
// destination file through importdecl and typeprint.
//
// Two forms nest text in a scope:
//
//   - [Block] pushes a scope and declares its identifiers before its body
//     renders. The body looks the identifiers up.
//   - [Wrap] runs after the text it encloses. The enclosed step asks for
//     an identifier with [Scope.Use], and the wrapping step claims it with
//     [Scope.Declare].
//
// The Parse and Print functions convert the text to dst or ast nodes and
// back. [DstExpr], [DstStmts], [DstDecls] and their ast counterparts are
// steps editing the text as nodes.
package genhelper
