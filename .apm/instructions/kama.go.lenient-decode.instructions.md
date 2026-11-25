---
description: "Generated decoders record problems and fail hard only when they cannot continue"
applyTo: ""
---

### Generated decoders defer failure

A decoder that a generator of this repository writes records what is wrong with its input and keeps decoding.
It reports every recorded problem when the decoded data is converted to the model.
It fails while unmarshaling only when it cannot continue, or when continuing would cost performance.

#### Rules

- Record a value that does not fit the Go type. Do not return an error for it while unmarshaling.
  - A value of the wrong JSON kind is recorded, such as a number for a string field.
  - A forbidden `null` is recorded.
  - A number that is not an integer is recorded for an integer field.
  - A missing required member is recorded.
- Do not catch the error of a nested unmarshal call. A nested generated value that fails stops the value that holds it, as `encoding/json` behaves.
- Report every recorded problem at the conversion from the wire form to the model, in one error, with the path of each.
- Fail while unmarshaling only in these cases:
  - The input is malformed, such as a syntax error or a truncated document.
  - The reader returns an error.
  - A nested generated value returns an error.
  - Recording would cost performance. Name the cost in a comment at that place.
- Do not work around the behavior of `encoding/json`. A wire struct is decoded by `encoding/json` through struct tags.
- Do not keep a value only to report it. Skip a value of the wrong kind, and put its kind and a short bounded excerpt in the error.
- Store what was converted in the destination, then return the error. The caller reads a partial value beside the error.
- Express a rule that `encoding/json` cannot judge as a type of the wire form, such as `wire.NonNullSlice[T]`. Do not pass unmarshalers through options for it.
  - A reader sees the rule in the type of the field.
  - The unmarshalers and marshalers of the caller then apply at every depth.

#### Why

- One run shows every problem of a document. The run-and-fix loop needs fewer rounds.
- A caller can print and inspect a document that does not fit, as in a dynamically typed language.
- A document written for an older schema stays readable, with the mismatches listed.
- Speed is not the first goal of this project. Clear and complete reports come first.

#### Shape

```go
type orderWire struct {
	Name  und.Und[wire.String]               `json:"name,omitzero"`
	Tags  und.Und[wire.NonNullSlice[string]] `json:"tags,omitzero"`
	Point und.Und[Point]                     `json:"point,omitzero"`
}

func (v *Order) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	var w orderWire

	// Malformed input and a failing nested Point return here.
	if err := kamaJsonschemaDecodeObject(dec, &w); err != nil {
		return err
	}

	var out Order
	var errs []error

	errs = append(errs, validator.Field(kamaJsonschemaRequiredOf(&out.Name, w.Name, wire.String.Value), "name"))
	errs = append(errs, validator.Field(kamaJsonschemaRequiredOf(&out.Tags, w.Tags, wire.NonNullSlice[string].Value), "tags"))
	errs = append(errs, validator.Field(kamaJsonschemaRequired(&out.Point, w.Point), "point"))

	// The destination holds what was converted, beside the error.
	*v = out

	return validator.Join(errs...)
}
```
