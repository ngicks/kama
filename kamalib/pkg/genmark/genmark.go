// Package genmark marks declarations emitted by kama generators.
//
// Every declaration a kama generator emits carries the directive comment
// //kama:generated <generator-name> in its doc comment. A generator
// registers its name in a Registry, and kama treats a declaration marked
// with a registered name as generated. A marker naming an unregistered
// generator does not make a declaration generated.
package genmark

import (
	"errors"
	"fmt"
	"go/ast"
	"strings"
	"sync"
	"unicode"

	"github.com/dave/dst"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
)

const (
	toolName  = "kama"
	directive = "generated"
)

var (
	ErrInvalidName = errors.New("genmark: invalid generator name")
	ErrDuplicate   = errors.New("genmark: duplicate generator name")
)

// DefaultRegistry is the Registry kama generators register their names in.
var DefaultRegistry = NewRegistry()

// Marker returns the directive comment marking a declaration as emitted by
// the generator name.
func Marker(name string) string {
	return "//" + toolName + ":" + directive + " " + name
}

// Registry holds the names of kama generators. It is safe for concurrent
// use.
type Registry struct {
	mu    sync.RWMutex
	names map[string]struct{}
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{names: make(map[string]struct{})}
}

// Register adds name to r. A valid name is non-empty and contains no
// whitespace, so that it round-trips through Marker as a single word.
// Register returns an error wrapping ErrInvalidName for an invalid name and
// ErrDuplicate for a name already registered.
func (r *Registry) Register(name string) error {
	if name == "" || strings.ContainsFunc(name, unicode.IsSpace) {
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.names[name]; ok {
		return fmt.Errorf("%w: %q", ErrDuplicate, name)
	}
	r.names[name] = struct{}{}
	return nil
}

// Generated reports the registered generator whose marker appears in cg.
// Text following the name in a marker is ignored.
func (r *Registry) Generated(cg *ast.CommentGroup) (name string, ok bool) {
	return r.find(dircmnt.Ast(cg))
}

// GeneratedDst reports the registered generator whose marker appears in the
// comments decorating the start of a node, after the last empty line.
func (r *Registry) GeneratedDst(decs dst.NodeDecs) (name string, ok bool) {
	return r.find(dircmnt.Dst(decs))
}

func (r *Registry) find(comments []dircmnt.Comment) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, c := range comments {
		if c.ToolName != toolName || c.Directive != directive {
			continue
		}
		fields := strings.Fields(c.Args)
		if len(fields) == 0 {
			continue
		}
		if _, ok := r.names[fields[0]]; ok {
			return fields[0], true
		}
	}
	return "", false
}
