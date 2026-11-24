package gofragment

import (
	"errors"

	"github.com/ngicks/kama/kamalib/pkg/gofragment/internal/wrap"
)

// ErrNotPartial reports text that does not parse as the partial text asked
// for: one expression, the statements of a function body or top-level
// declarations other than imports. The Parse functions return it for text
// escaping the file they wrap it in, such as statements closing the
// function body.
var ErrNotPartial = wrap.ErrNotPartial

// ErrNoFileSet reports an ast function without a token.FileSet. The ast
// Parse and Print functions return it for a nil file set.
var ErrNoFileSet = errors.New("gofragment: no file set")
