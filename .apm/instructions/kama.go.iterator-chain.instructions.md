---
description: "Iterator chains with github.com/ngicks/opiter"
applyTo: ""
---

### Iterator chains

Go code in this repository transforms sequences with `github.com/ngicks/opiter` method chains.

#### When to use a chain

- Write a chain for a filter, a map, a filter followed by a map, any / every / contains, find / first, unique, reduce / sum, flatten, and iteration over a map in key order.
- Keep a plain `for` loop for these cases:
  - The body returns an error that carries per-item context.
  - The body builds a set (`m[k] = true`) or mutates captured state.
  - The body is mostly side effects.
  - The loop needs index bookkeeping, or it is a recursive visitor.
  - The loop is a table-driven test loop, or it asserts per item.
- Do not force a chain where it reads worse than the loop.

#### Shape of a chain

- Enter a chain through `opiter.WrapSeq` or `opiter.WrapSeq2`.
- Put the source on its own line inside the parentheses.
  - Keep a source that is a bare variable on the opening line, as in `opiter.WrapSeq(specs).`.
- Put every method on its own line. This holds for short chains and for chains with one method.
- End a chain with `.Collect()` to get a slice.
- End a chain with `.Iter()` or `.Iter2()` only when the consumer takes `iter.Seq` or `iter.Seq2`, such as `slices.Sorted`, `slices.AppendSeq`, `maps.Collect` or the `Concat` method.

```go
out := opiter.WrapSeq2(
	opiter.MapsSorted(byPath),
).
	OmitF().
	Map(
		func(h *Host) Host { return *h },
	).
	Collect()
```

- gofmt indents the method lines one tab deeper than the first line. Keep that indentation.
- A single method call on an intermediate `opiter.Seq` variable stays on one line, as in `users.First()`.
- Move a chain into a named variable when it would sit in an `if` init or a `for` header.

#### Helper functions of opiter

- Some steps have no method, such as `opiter.TryCollect`, `opiter.Flatten`, `opiter.Min`, `opiter.MapsSorted`, `opiter.Range` and `opiter.Once`. Call them as functions around or inside the chain.
- Put each argument of such a call on its own line.

```go
names, err := opiter.TryCollect(
	opiter.WrapSeq(
		slices.Values(tps),
	).
		Disassemble(render),
)
```

#### Predicates, mappers and reducers

- Inline a light function as a func literal. Put the literal on its own line inside the parentheses of the method.
- A function is light when all of these hold:
  - Its body is one short `return` expression or one expression statement.
  - The literal fits on one line within the line length limit.
  - Exactly one chain uses it.
- Delete the named function after inlining it.
- Keep a function named in these cases:
  - Two or more sites use it.
  - Its body has more than one statement, or it does not fit on one line.
  - It carries a comment that explains a non-obvious reason. Alternatively, inline it and move the comment above the chain.
- Pass an existing function, method value or method expression directly, as in `Map(strconv.Quote)` or `Map(Tag.String)`.
- Name a kept function by its meaning, such as `hasFlag`, `notSkipped` or `toStep`. Do not name it `f`, `fn` or `pred`.
- Declare a kept function at package level when it captures nothing. Declare it as a local closure directly above the chain when it captures locals.
