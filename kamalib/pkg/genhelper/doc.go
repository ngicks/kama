// Package genhelper renders Go source text through a chain of steps.
//
// A [Step] receives the text the steps before it produced and appends to
// it, wraps it or replaces it. [Steps] runs steps in order and is a step
// itself, so chains nest. The text is partial: an expression, statements or
// declarations. [WrapFile] turns declarations into a whole file, and
// AppendDecls of package dstgen adds them to an existing dst file.
//
// [GeneratorParams.Comment] builds the standard header of a generated
// file, which [WrapFile] and [PrintFileHeader] print before the package
// clause. [SameExceptGenerated] compares two generated files ignoring the
// time of the run the header records.
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
// [For], [ForRange], [If] and [Switch] are steps for common statements. Each
// renders its bodies in scopes of their own. [Func] is a step for a
// function, whose signature and body share one scope. [DocFunc] adds a doc
// comment, which renders in that scope after the signature and so spells
// the names of the parameters.
//
// [Expr] formats a piece of text, and [Stmt] and [Line] append one line.
// The arguments [Ref], [Decl], [Use] and [Shadow] stand for the name of a
// role in the scope the text renders in. A [Gen] argument stands for its
// text. The format string is source text the generator author writes, and
// generated text goes in as an argument. [Join] joins texts with a
// separator, and [Comment] turns texts into one comment group.
//
// [Fail] and [FailGen] fail when they render, for a branch that has no text
// to give. [Render] renders steps to a string.
//
// [Expr], [Stmt], [Name], [Fail] and [FailGen] take no argument of the
// context type, so a caller instantiates them, as in Stmt[C]. [Join] and
// [Comment] without parts need it too. [Line], [Func], [DocFunc],
// [For], [ForRange], [If], [Switch], [Join], [Comment] and [Render] infer the
// context from their arguments.
//
// The vocabulary comes in two forms that render the same text. The
// package-level functions take the context type as a type argument where
// they cannot infer it, and a consumer may declare thin wrappers for its
// context once:
//
//	type genCtx = genhelper.BaseContext
//
//	func stmt(format string, args ...any) genhelper.Step[genCtx] {
//		return genhelper.Stmt[genCtx](format, args...)
//	}
//
// The methods of [Lang] fix the context type in the type of a Lang value,
// so no call names it:
//
//	var l genhelper.Lang[genCtx]
//
//	l.Func(
//		l.Expr("func (%s T) Len() int", genhelper.Decl("t")),
//		l.Stmt("return len(%s)", genhelper.Ref("t")),
//	)
//
// Lang also builds the clauses of [Switch] with [Lang.Case] and
// [Lang.Default].
//
// Package [gofragment] converts the text to ast nodes and back. [AstExpr],
// [AstStmts] and [AstDecls] are steps editing the text as ast nodes
// through it. Package dstgen holds their dst counterparts, so that
// genhelper does not make dst a dependency of its importers.
package genhelper
