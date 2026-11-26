// Package genmark marks declarations emitted by code generators.
//
// Every declaration a generator of a tool emits carries the directive
// comment //<tool-name>:generated <generator-name> in its doc comment. A
// Registry belongs to one tool. A generator registers its name in the
// Registry of its tool, and the Registry reports a declaration marked with
// that tool and a registered name as generated. A marker naming another
// tool or an unregistered generator does not make a declaration generated.
package genmark

import (
	"errors"
	"fmt"
	"go/ast"
	"strings"
	"sync"
	"unicode"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
	"github.com/ngicks/opiter"
)

const directive = "generated"

var (
	ErrInvalidName = errors.New("genmark: invalid generator name")
	ErrDuplicate   = errors.New("genmark: duplicate generator name")
)

// Marker returns the directive comment marking a declaration as emitted by
// the generator name of the tool toolName.
func Marker(toolName, name string) string {
	return "//" + toolName + ":" + directive + " " + name
}

// Registry holds the names of the generators of one tool. It is safe for
// concurrent use.
type Registry struct {
	toolName string
	mu       sync.RWMutex
	names    map[string]struct{}
}

// NewRegistry returns an empty Registry of the tool toolName.
func NewRegistry(toolName string) *Registry {
	return &Registry{toolName: toolName, names: make(map[string]struct{})}
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

// Registered reports whether name is registered in r.
func (r *Registry) Registered(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, ok := r.names[name]

	return ok
}

// Unregistered reports the first marker of the tool of r in cg naming a
// generator r does not register, such as a marker written before a
// generator was renamed. A marker without a name counts as one naming the
// empty name.
func (r *Registry) Unregistered(cg *ast.CommentGroup) (name string, ok bool) {
	return opiter.SlicesValues(dircmnt.Ast(cg)).
		Filter(r.isMarker).
		Map(markerName).
		Filter(func(name string) bool { return !r.Registered(name) }).
		First().
		Unpack()
}

func (r *Registry) isMarker(c dircmnt.Comment) bool {
	return c.ToolName == r.toolName && c.Directive == directive
}

func (r *Registry) find(comments []dircmnt.Comment) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Register rejects the empty name, so a marker without a name never
	// counts as registered.
	isRegistered := func(name string) bool {
		_, ok := r.names[name]
		return ok
	}

	return opiter.SlicesValues(comments).
		Filter(r.isMarker).
		Map(markerName).
		Filter(isRegistered).
		First().
		Unpack()
}

func markerName(c dircmnt.Comment) string {
	fields := strings.Fields(c.Args)
	if len(fields) == 0 {
		return ""
	}

	return fields[0]
}
