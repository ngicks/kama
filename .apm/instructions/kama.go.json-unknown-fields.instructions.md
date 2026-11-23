---
description: "Unknown fields in decoded files: capture and warn, never reject"
applyTo: ""
---

### Unknown fields are captured and warned about

This repository never rejects an unknown field of a decoded document.
This rule covers option files, workflow specs, proc params and every other file a user writes.
The general Go rule allows rejection when a project requires strictness. This project does not require it.

#### Rules

- Never use the options that reject unknown fields.
  - Do not use v2 `json.RejectUnknownMembers(true)`.
  - Do not use v1 `(*json.Decoder).DisallowUnknownFields`.
- Capture unknown fields in every decoded struct with a v2 embedded fallback field: `` Unknown map[string]any `json:",embed"` ``.
- Report each captured field as a warning. Do not report it as an error.
  - A warning names the full key path of the field.
  - The run continues after the warning.
- Apply the same rule to a hand-written check for unknown keys, such as a check over a `map[string]any` table.
- Keep the error for a value that is known and invalid, such as an unsupported `version`.

#### Why

- Rejection breaks forward compatibility. A file written for a newer version fails in an older tool.
- Rejection breaks backward compatibility. A file that keeps a removed key fails in a newer tool.
- A warning still tells the user about a typo.

#### Shape

```go
type TypeOptions struct {
	Ignore  bool           `json:"ignore"`
	Unknown map[string]any `json:",embed"`
}
```

The loader reads `Unknown` after decoding and appends one warning for each key.
