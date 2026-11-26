package genhelper

import (
	"fmt"
	"go/types"
	"maps"
	"slices"
	"strconv"

	"github.com/ngicks/opiter"
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
//
// [Scope.LinkImports] shares the names with the imports of the file, so
// that locals and import identifiers do not collide.
type Scope struct {
	parent   *Scope
	children []*Scope
	// imports is set on the root scope only, so that every scope of the
	// tree, including those pushed later, shares one link.
	imports ImportNames
	// names are the names this scope holds: reserved, declared or pending.
	names map[string]bool
	roles map[string]binding
}

type binding struct {
	name string
	// pending marks a name Use handed out and no Declare has claimed.
	pending bool
}

// ImportNames is the import side of the identifiers a file holds.
// *importdecl.Map implements it.
type ImportNames interface {
	// HasIdent reports whether an import of the file is accessed through
	// ident.
	HasIdent(ident string) bool
	// Reserve marks idents as taken, so that imports added later avoid them.
	Reserve(idents ...string)
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

// LinkImports links the scope tree s belongs to with names, the imports
// of the file the generated text lands in. Every scope of the tree, those
// pushed later included, shares the link.
//
// Every name the tree holds, now or later, is reserved in names, so that
// imports added later avoid it. A fresh name [Scope.Declare], [Scope.Use]
// or [Scope.LookupOrDeclare] hands out avoids the identifiers of the
// imports. [Scope.Shadow] does not.
//
// Linking again replaces the link. A nil names unlinks the tree.
func (s *Scope) LinkImports(names ImportNames) {
	root := s.root()

	root.imports = names
	if names != nil {
		root.pushNames(names)
	}
}

func (s *Scope) pushNames(names ImportNames) {
	names.Reserve(slices.Sorted(maps.Keys(s.names))...)

	for _, child := range s.children {
		child.pushNames(names)
	}
}

func (s *Scope) root() *Scope {
	for s.parent != nil {
		s = s.parent
	}

	return s
}

// hold adds names to s and reserves them in the linked imports.
func (s *Scope) hold(names ...string) {
	for _, name := range names {
		s.names[name] = true
	}

	if imports := s.root().imports; imports != nil {
		imports.Reserve(names...)
	}
}

// Reserve marks names as taken in s without giving them a role.
func (s *Scope) Reserve(names ...string) {
	s.hold(names...)
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
	s.hold(name)
	s.roles[role] = binding{name: name}

	return name
}

// Shadow declares the identifier of role in s under exactly the name role,
// even when a scope enclosing s holds that name. Text rendered in s and
// the scopes nested in it no longer reaches what the name referred to
// outside, such as the builtin new. Shadow may take the identifier of an
// import linked by [Scope.LinkImports], and the import is then unreachable
// in s and the scopes nested in it.
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

	s.hold(role)
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
	s.hold(name)
	s.roles[role] = binding{name: name, pending: true}

	return name
}

// Unbound returns the roles of s and the scopes nested in it that Use
// handed a name for and no Declare claimed, sorted.
func (s *Scope) Unbound() []string {
	out := s.appendUnbound(nil)
	slices.Sort(out)

	return slices.Compact(out)
}

func (s *Scope) appendUnbound(out []string) []string {
	out = opiter.MapsAll(s.roles).
		Filter(func(_ string, b binding) bool { return b.pending }).
		OmitL().
		AppendSeq(out)

	for _, child := range s.children {
		out = child.appendUnbound(out)
	}

	return out
}

func (s *Scope) fresh(hint string) string {
	name := hint
	for i := 2; s.taken(name); i++ {
		name = hint + strconv.Itoa(i)
	}

	return name
}

func (s *Scope) taken(name string) bool {
	if imports := s.root().imports; imports != nil && imports.HasIdent(name) {
		return true
	}

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

	return opiter.SlicesValues(s.children).
		ContainsFunc(func(child *Scope) bool { return child.takenBelow(name) })
}
