// Package gofragment converts fragments of Go source text to ast nodes and
// back. Package dstfragment does the same with dst nodes.
//
// A fragment has no kind of its own. The function converting it names the
// kind: an expression, a list of statements or a list of declarations.
//
// The Parse functions parse the text inside a throwaway file and check the
// file kept the shape they wrapped the text in. Text closing the wrapper and
// adding to the file, such as statements ending the function body they
// are parsed in, is an error wrapping [ErrNotPartial]. Syntax errors of the
// Go parser are wrapped. Their positions refer to the throwaway file, not
// to the text.
//
// The Print functions print partial text without the wrapper. The caller
// manages the imports of the file a fragment goes into. The declaration
// Parse functions reject import declarations.
//
// The ast functions suit reading and type checking. Comments of ast nodes
// are placed by position, and nodes parsed from partial text carry the
// positions of the throwaway file wrapping that text. Comments drift when
// such nodes move into another file. The ast Print functions print only
// the comments node fields hold, such as the Doc of a declaration. The dst
// functions of dstfragment keep comments attached to their nodes.
package gofragment
