package jsonschema

import (
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/ngicks/option/opt"

	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// bundle returns doc with every document a reference of doc names outside
// doc embedded under $defs of the root, keyed by its retrieval URI, and so
// on for the references of the documents embedded. The embedded root takes
// its own $id resolved against its retrieval URI, or the retrieval URI when
// it has none, so the references inside it resolve as they would in its own
// document. A cycle of references across documents ends, since a document
// already embedded is not loaded again.
//
// The result is a compound document as JSON Schema bundles one, not a copy
// of facts derived from doc: the generator reads it as its only source, so
// a schema of another document gets a pointer and a name as any other
// schema does.
func bundle(doc *jsonschema.Schema, opts Options) (*jsonschema.Schema, error) {
	for {
		ptr, kw, uri, ok := missingDocument(doc)
		if !ok {
			return doc, nil
		}

		s, err := at(doc, ptr)
		if err != nil {
			return nil, err
		}

		ref, _ := refOf(s, kw)

		if opts.Load == nil {
			return nil, unsupported(child(ptr, kw), "reference %q names another document", ref)
		}

		loaded, err := opts.Load(uri)
		if err != nil {
			return nil, unsupported(child(ptr, kw), "reference %q: %v", ref, err)
		}

		if doc.Bool.IsSome() || loaded.Bool.IsSome() {
			return nil, unsupported(child(ptr, kw),
				"reference %q joins a boolean schema with another document", ref)
		}

		id, err := jsonschema.BaseUri(loaded, uri, "")
		if err != nil {
			return nil, unsupported(child(ptr, kw), "reference %q: %v", ref, err)
		}

		embedded := *loaded
		embedded.Id = opt.Some(id)

		root := *doc
		root.Defs = append(
			slices.Clip(doc.Defs),
			jsonschema.NamedSchema{Name: uri, Schema: &embedded},
		)
		doc = &root
	}
}

// missingDocument returns the first reference of doc, as the pointer of the
// schema holding it and the keyword $ref or $dynamicRef, whose URI names no
// schema resource of doc and no document bundle embedded, along with that
// URI without its fragment.
func missingDocument(doc *jsonschema.Schema) (string, string, string, bool) {
	for ptr, s := range schemas(doc) {
		if s == nil {
			continue
		}

		for _, kw := range []string{"$ref", "$dynamicRef"} {
			ref, ok := refOf(s, kw)
			if !ok {
				continue
			}

			target, err := jsonschema.RefUri(doc, "", ptr, ref)
			if err != nil {
				continue
			}

			uri, _, _ := strings.Cut(target, "#")

			if _, embedded := doc.Defs.Get(uri); embedded || isResource(doc, uri) {
				continue
			}

			return ptr, kw, uri, true
		}
	}

	return "", "", "", false
}

// refOf returns the reference the keyword kw of s, $ref or $dynamicRef,
// holds, and whether s holds kw.
func refOf(s *jsonschema.Schema, kw string) (string, bool) {
	if kw == "$dynamicRef" {
		return s.DynamicRef.Unpack()
	}

	return s.Ref.Unpack()
}

// isResource reports whether uri names a schema resource of doc.
func isResource(doc *jsonschema.Schema, uri string) bool {
	for id := range jsonschema.Resources(doc, "") {
		if id == uri {
			return true
		}
	}

	return false
}

// embeddedDocument returns the retrieval URI of the document bundle
// embedded at ptr, a schema under $defs of the root, or "" when ptr is no
// such schema. A key of $defs of the root that is an absolute URI names an
// embedded document, as bundle keys them.
func embeddedDocument(ptr string) string {
	rest, ok := strings.CutPrefix(ptr, "/$defs/")
	if !ok || strings.Contains(rest, "/") {
		return ""
	}

	key := unescape(rest)

	u, err := url.Parse(key)
	if err != nil || !u.IsAbs() {
		return ""
	}

	return key
}

// documentName returns the Go name of the document embedded at ptr: the
// base name of the path of its retrieval URI uri without its extension, so
// http://example.com/schemas/pet.json gives Pet.
func documentName(ptr, uri string) (string, error) {
	u, _ := url.Parse(uri)

	p := u.Path
	if u.Opaque != "" {
		p = u.Opaque
	}

	base := path.Base(strings.TrimSuffix(p, "/"))

	return identifier(ptr, "document", strings.TrimSuffix(base, path.Ext(base)))
}
