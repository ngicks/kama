// Package genhelper renders Go source text through a chain of steps.
//
// A [Step] receives the text the steps before it produced and appends to
// it, wraps it or replaces it. [Steps] runs steps in order and is a step
// itself, so chains nest. The text is partial: an expression, statements or
// declarations. [WrapFile] turns declarations into a whole file, and
// [AppendDecls] adds them to an existing one.
//
// Steps share a [Context]. Its [Scope] hands out identifiers by role, so
// independently written steps declare locals without duplicating or
// shadowing names. Its [Package] spells package qualifiers and types for
// the destination file through importdecl and typeprint. Its [Printer]
// builds the text of a step. [NewBaseContext] links the Scope with the
// imports of the Package, so that a local never takes the identifier of an
// import and an import added later avoids the names of locals.
//
// Two forms nest text in a scope:
//
//   - [Block] pushes a scope and declares its identifiers before its body
//     renders. The body looks the identifiers up.
//   - [Wrap] runs after the text it encloses. The enclosed step asks for
//     an identifier with [Scope.Use], and the wrapping step claims it with
//     [Scope.Declare].
//
// [ForRange], [If] and [Switch] are steps for common statements. Each
// renders its bodies in scopes of their own. [Func] is a step for a
// function, whose signature and body share one scope.
//
// [Expr] formats a piece of text, and [Stmt] and [Line] append one line.
// The arguments [Ref], [Decl], [Use] and [Shadow] stand for the name of a
// role in the scope the text renders in. A [Gen] argument stands for its
// text. The format string is source text the generator author writes, and
// generated text goes in as an argument.
//
// [Expr], [Stmt] and [Name] take no argument of the context type, so a
// caller instantiates them, as in Stmt[C]. A consumer declares thin
// wrappers once for its context:
//
//	type genCtx = genhelper.BaseContext
//
//	type (
//		ref  = genhelper.Ref
//		decl = genhelper.Decl
//	)
//
//	func expr(format string, args ...any) genhelper.Gen[genCtx] {
//		return genhelper.Expr[genCtx](format, args...)
//	}
//
//	func stmt(format string, args ...any) genhelper.Step[genCtx] {
//		return genhelper.Stmt[genCtx](format, args...)
//	}
//
// [Line], [Func], [ForRange], [If] and [Switch] infer the context from
// their [Gen] or [Step] arguments.
//
// The Parse and Print functions convert the text to dst or ast nodes and
// back. [DstExpr], [DstStmts], [DstDecls] and their ast counterparts are
// steps editing the text as nodes.
package genhelper
