# JSON-Schema-Test-Suite (vendored)

This directory holds a partial copy of the JSON Schema Test Suite.
The tests of `kamalib/pkg/jsonschema` read the draft 2020-12 test cases from it.

Source: https://github.com/json-schema-org/JSON-Schema-Test-Suite

Commit: `245a45abc90190c0bff04d97e6d2b541b4bb9f12`

## Contents

The copy keeps these paths of the upstream tree unchanged:

- `tests/draft2020-12/`, including `optional/`
- `remotes/`
- `LICENSE`

Upstream licenses the suite under the MIT license in `LICENSE`.

## Refresh

Run this at the repository root:

```sh
just refresh-jsonschema-suite <commit-or-ref>
```

The recipe replaces the copied paths and rewrites the commit line above.
Upstream replaced `tests/draft2020-12/` and `remotes/` with a new `validation/tests/` layout after the pinned commit.
The recipe rejects a ref that lacks the copied paths.
