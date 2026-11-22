---
description: "Single source of truth in code generators"
applyTo: ""
---

### Single source of truth in generators

A code generator in this repository reads everything from its source: the syntax trees, the type information, the directives and the option files.
The source is the only place a fact lives.

#### Rules

- Do not build intermediate data between the source and the code that emits output.
  - Do not classify the source up front into enums, kinds or flags that later code switches on.
  - Do not collect per-type or per-field records in one pass and hand them to another pass.
- Walk `go/ast` and `go/types` again wherever a fact is needed. Walk them as often as needed.
  - Performance is not critical in a generator. Repeated walks are the expected cost.
- Do not define a cache type.
- Do not define a type whose only job is to carry derived data from one function to another.
- Let the consumer ask for the data it needs.
  - Each writer requests a small piece of data at the point of use.
  - The generator composes that piece on the fly from the source and drops it after use.
- Share logic as a function over the source. Do not share its results.
  - Two passes that need the same answer call the same function on the same type.

#### When an intermediate or a cache is allowed

A new cache type or intermediate data object needs a strong justification. Write that justification in a comment on the type. These justifications count:

- The generator is significantly slow without it, and the difference is measured.
  - A cache that brings a run from tens of minutes down to a few minutes or to seconds qualifies.
  - A run that is already fast does not qualify, however many walks the cache saves.
- The data cannot be derived from the source. The object is then a new source of truth, and no other copy of that data may exist.

A guess that repeated walks are slow is not a justification. Measure first.

An example of a justified data structure:

- Directive comments are compiled down to a data structure.
  - An option reaches a generator from a directive comment or from an option file.
  - The generator must handle both transparently, so both are decoded into one option structure.

#### Shape

Match the type where the fact is needed:

```go
for v := range st.Fields() {
	m := sel.matchField(v, pkgPath)
	if m.blank {
		continue
	}

	// Emit for v from m. Nothing keeps m.
}
```

Do not compute it ahead and pass it along:

```go
// fieldInfo copies facts that the types already hold.
type fieldInfo struct {
	conv  conv
	class class
}

infos := mapFields(st)
writeMethods(infos)
```
