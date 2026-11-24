---
description: "Functions return a new model and do not mutate their inputs"
applyTo: ""
---

### Functions return a new model

A function that combines or transforms models returns a new model.
It does not write to the models it receives.

#### Rules

- Return the result. Do not take a pointer to a model only to write the result into it.
  - Write `merged, warnings := overlay(lower, upper)`.
  - Do not write `warnings := overlay(&lower, upper)`.
- Do not write to a map or a slice that an argument holds. Clone the container, then write to the clone.
  - `maps.Clone` and `slices.Clone` copy one level. That level is enough when the function replaces entries and does not edit them.
- Share the values below the cloned level when nothing writes to them. State the sharing in the doc comment.
- A function may fill a value that it created itself and has not returned yet.
- Keep a mutating method where mutation is the purpose of the type, such as a builder, a decoder target or an `Unmarshal` method.

#### Why

- A caller can keep and reuse an input after the call.
- A reader sees every change of a model at an assignment.
- A pure function is tested with plain inputs and outputs.

#### Shape

```go
// overlay returns lower with the types of upper set on it. It writes to
// neither. The result shares the options of the types with them.
func overlay(lower, upper map[string]TypeOption) (map[string]TypeOption, []error) {
	out := maps.Clone(lower)
	if out == nil {
		out = map[string]TypeOption{}
	}

	var warnings []error

	for _, name := range slices.Sorted(maps.Keys(upper)) {
		if prev, ok := out[name]; ok {
			warnings = append(warnings, overridden(name, upper[name].Source, prev.Source))
		}

		out[name] = upper[name]
	}

	return out, warnings
}
```
