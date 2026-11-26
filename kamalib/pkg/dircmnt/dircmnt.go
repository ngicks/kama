// Package dircmnt extracts directive comments from go/ast comments.
//
// A directive comment is a line comment //toolname:directive args, as in
// //go:generate go run ./gen. The toolname: part and the args are optional,
// as in //go:noinline or //line x.go:10. A space after //, an empty toolname
// or directive, or a block comment makes it an ordinary comment.
//
// [Parse] parses one comment, and [Ast] finds the directive comments of a
// comment group. The package leaves the
// args as written. [ParseArgsKeyValue] parses args written as
// space-separated key=value pairs, as of //kama:option-file
// partial=kama_partial.toml. The expr package parses args written as
// Starlark expressions, as the directives of the generators of this module
// are.
//
// Lifted from github.com/ngicks/go-codegen/codegen/pkg/directive.
package dircmnt

import (
	"go/ast"
	"iter"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ngicks/opiter"
)

// Comment is a parsed directive comment //ToolName:Directive Args.
type Comment struct {
	// ToolName is empty for a directive without a colon, such as //line.
	ToolName  string
	Directive string
	// Args is the text after the first whitespace following Directive.
	//
	// Args stays as written because the args of a directive follow the
	// grammar of its tool, as the command line of //go:generate does.
	Args string
}

// Parse parses text, a comment including its marker, as a directive
// comment.
func Parse(text string) (Comment, bool) {
	body, ok := strings.CutPrefix(text, "//")
	if !ok {
		return Comment{}, false
	}

	word, args := body, ""
	if i := strings.IndexFunc(body, unicode.IsSpace); i >= 0 {
		_, size := utf8.DecodeRuneInString(body[i:])
		word, args = body[:i], body[i+size:]
	}

	toolName, directive, hasColon := strings.Cut(word, ":")
	if !hasColon {
		toolName, directive = "", word
	} else if toolName == "" {
		return Comment{}, false
	}

	if directive == "" {
		return Comment{}, false
	}

	return Comment{ToolName: toolName, Directive: directive, Args: args}, true
}

// Ast returns the directive comments in cg.
func Ast(cg *ast.CommentGroup) []Comment {
	return collect(astTexts(cg))
}

func astTexts(cg *ast.CommentGroup) iter.Seq[string] {
	if cg == nil {
		return opiter.Empty[string]()
	}

	return opiter.WrapSeq(slices.Values(cg.List)).
		Map(func(c *ast.Comment) string { return c.Text }).
		Iter()
}

func collect(texts iter.Seq[string]) []Comment {
	return opiter.WrapSeq(texts).
		Disassemble(Parse).
		Filter(func(_ Comment, ok bool) bool { return ok }).
		OmitL().
		CollectSlice()
}
