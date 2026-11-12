package genhelper

import (
	"fmt"
	"go/types"
	"slices"
	"strconv"
)

// Scope is a lexical scope of the generated code. It hands out identifiers
// that neither duplicate nor shadow one another.
//
// An identifier is asked for by role, the name the generator knows it by.
// The role is also the name tried first. A taken name gets a number
// appended: v, v2, v3.
//
// A name is taken when this scope, a scope enclosing it or a scope nested
// in it holds it. Scopes beside each other reuse names.
type Scope struct {
	parent   *Scope
	children []*Scope
	// names are the names this scope holds: reserved, declared or pending.
	names map[string]bool
	roles map[string]binding
}

type binding struct {
	name string
	// pending marks a name Use handed out and no Declare has claimed.
	pending bool
}

// NewScope returns a root scope.
func NewScope() *Scope {
	return &Scope{names: map[string]bool{}, roles: map[string]binding{}}
}

// Push returns a new scope nested in s.
func (s *Scope) Push() *Scope {
	child := NewScope()
	child.parent = s
	s.children = append(s.children, child)
	return child
}

// Parent returns the scope enclosing s, or nil for a root scope.
func (s *Scope) Parent() *Scope {
	return s.parent
}

// Reserve marks names as taken in s without giving them a role.
func (s *Scope) Reserve(names ...string) {
	for _, name := range names {
		s.names[name] = true
	}
}

// ReserveUniverse reserves the predeclared identifiers.
func (s *Scope) ReserveUniverse() {
	s.Reserve(types.Universe.Names()...)
}

// ReserveTypes reserves the names of ts, usually the package scope of the
// destination.
func (s *Scope) ReserveTypes(ts *types.Scope) {
	s.Reserve(ts.Names()...)
}

// Declare declares the identifier of role in s and returns its name.
//
// A pending name [Scope.Use] handed out for role in s becomes the declared
// one. Declaring a role s already declares gives a new name, and the role
// refers to the new name from then on.
func (s *Scope) Declare(role string) string {
	if b, ok := s.roles[role]; ok && b.pending {
		s.roles[role] = binding{name: b.name}
		return b.name
	}
	name := s.fresh(role)
	s.names[name] = true
	s.roles[role] = binding{name: name}
	return name
}

// Shadow declares the identifier of role in s under exactly the name role,
// even when a scope enclosing s holds that name. Text rendered in s and
// the scopes nested in it no longer reaches what the name referred to
// outside, such as the builtin new.
//
// Shadow returns an error wrapping [ErrRedeclared] when s itself holds the
// name, which Go rejects as a redeclaration, and when [Scope.Use] already
// handed out another name for role in s.
func (s *Scope) Shadow(role string) (string, error) {
	if b, ok := s.roles[role]; ok && b.pending {
		if b.name != role {
			return "", fmt.Errorf("%w: %s is already used as %s", ErrRedeclared, role, b.name)
		}
		s.roles[role] = binding{name: role}
		return role, nil
	}
	if s.names[role] {
		return "", fmt.Errorf("%w: %s", ErrRedeclared, role)
	}
	s.names[role] = true
	s.roles[role] = binding{name: role}
	return role, nil
}

// Lookup returns the name role refers to in s or the nearest scope
// enclosing it.
func (s *Scope) Lookup(role string) (name string, ok bool) {
	for sc := s; sc != nil; sc = sc.parent {
		if b, ok := sc.roles[role]; ok {
			return b.name, true
		}
	}
	return "", false
}

// LookupOrDeclare returns the name role refers to in s or a scope
// enclosing it, declaring it in s when nothing declares it. declared tells
// the caller to emit a declaration instead of an assignment.
//
// A pending name [Scope.Use] handed out in s counts as undeclared, so
// LookupOrDeclare claims it.
func (s *Scope) LookupOrDeclare(role string) (name string, declared bool) {
	if b, ok := s.roles[role]; ok && b.pending {
		return s.Declare(role), true
	}
	if name, ok := s.Lookup(role); ok {
		return name, false
	}
	return s.Declare(role), true
}

// Use returns the name role refers to, as Lookup does. For a role nothing
// declares yet, Use hands out a pending name, which a later Declare of the
// role in s claims.
//
// Use serves a step whose text is wrapped by a later step declaring the
// identifier.
func (s *Scope) Use(role string) string {
	if name, ok := s.Lookup(role); ok {
		return name
	}
	name := s.fresh(role)
	s.names[name] = true
	s.roles[role] = binding{name: name, pending: true}
	return name
}

// Unbound returns the roles of s and the scopes nested in it that Use
// handed a name for and no Declare claimed, sorted.
func (s *Scope) Unbound() []string {
	var out []string
	for role, b := range s.roles {
		if b.pending {
			out = append(out, role)
		}
	}
	for _, child := range s.children {
		out = append(out, child.Unbound()...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func (s *Scope) fresh(hint string) string {
	name := hint
	for i := 2; s.taken(name); i++ {
		name = hint + strconv.Itoa(i)
	}
	return name
}

func (s *Scope) taken(name string) bool {
	for sc := s.parent; sc != nil; sc = sc.parent {
		if sc.names[name] {
			return true
		}
	}
	return s.takenBelow(name)
}

func (s *Scope) takenBelow(name string) bool {
	if s.names[name] {
		return true
	}
	return slices.ContainsFunc(s.children, func(child *Scope) bool {
		return child.takenBelow(name)
	})
}
