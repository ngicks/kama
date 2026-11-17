// Package dircmnt extracts directive comments from go/ast and dst
// decorations.
//
// A directive comment is a line comment //toolname:directive args, as in
// //go:generate go run ./gen. The toolname: part and the args are optional,
// as in //go:noinline or //line x.go:10. A space after //, an empty toolname
// or directive, or a block comment makes it an ordinary comment.
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

	"github.com/dave/dst"
	"github.com/ngicks/opiter"
)

// Comment is a parsed directive comment //ToolName:Directive Args.
type Comment struct {
	// ToolName is empty for a directive without a colon, such as //line.
	ToolName  string
	Directive string
	// Args is the text after the first whitespace following Directive.
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

// AstField returns the directive comments of f, from its doc comment
// followed by the first comment of its line comment.
func AstField(f *ast.Field) []Comment {
	var trailing []string
	if f.Comment != nil && len(f.Comment.List) > 0 {
		trailing = []string{f.Comment.List[0].Text}
	}

	return collect(
		opiter.WrapSeq(astTexts(f.Doc)).
			Concat(slices.Values(trailing)).
			Iter(),
	)
}

// Dst returns the directive comments decorating the start of a node, after
// the last empty line.
func Dst(decs dst.NodeDecs) []Comment {
	return collect(slices.Values(afterLastEmptyLine(decs.Start)))
}

// DstField returns the directive comments of a field, from the comments
// decorating its start after the last empty line, followed by the first
// comment decorating its end.
func DstField(decs dst.FieldDecorations) []Comment {
	return collect(
		opiter.WrapSeq(slices.Values(afterLastEmptyLine(decs.Start))).
			Concat(slices.Values(decs.End[:min(len(decs.End), 1)])).
			Iter(),
	)
}

func astTexts(cg *ast.CommentGroup) iter.Seq[string] {
	if cg == nil {
		return opiter.Empty[string]()
	}

	return opiter.WrapSeq(slices.Values(cg.List)).
		Map(func(c *ast.Comment) string { return c.Text }).
		Iter()
}

// afterLastEmptyLine drops the decorations up to and including the last
// empty line, which separates comments not attached to the node.
func afterLastEmptyLine(lines []string) []string {
	for i, s := range slices.Backward(lines) {
		// dst only records a strictly empty "\n"; a line holding only spaces
		// is not reported as empty.
		if s != "\n" {
			continue
		}
		// dst records a line break after a block comment as "\n" too, so a
		// "\n" right after one is not an empty line.
		if i > 0 && strings.HasPrefix(lines[i-1], "/*") {
			continue
		}

		return lines[i+1:]
	}

	return lines
}

func collect(texts iter.Seq[string]) []Comment {
	return opiter.WrapSeq(texts).
		Disassemble(Parse).
		Filter(func(_ Comment, ok bool) bool { return ok }).
		OmitL().
		CollectSlice()
}
