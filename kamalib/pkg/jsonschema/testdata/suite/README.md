# JSON-Schema-Test-Suite (vendored)

This directory holds a partial copy of the JSON Schema Test Suite.
The tests of this module read the test cases that apply to draft 2020-12 from it through package `kamalib/internal/suitetest`.

Source: https://github.com/json-schema-org/JSON-Schema-Test-Suite

Commit: `e2a68a15cd490750fd6b95e604e4054d7d978d1f`

## Contents

The copy keeps these paths of the upstream tree unchanged:

- `validation/tests/`, including `optional/` and without `proposals/`
- `validation/README.md`, which defines the layout of the test files
- `validation/validation-test-schema.json`, the JSON Schema of a test file
- `LICENSE`

Upstream licenses the suite under the MIT license in `LICENSE`.

`proposals/` holds tests of keywords proposed for a future release.
Its test cases are compatible with no released dialect, so the copy leaves it out.

## Layout

Upstream holds a single tree of validation tests for every dialect under `validation/tests/`.
It no longer has a `tests/<draft>/` tree per dialect or a `remotes/` directory.
`annotations/` holds a separate suite of annotation tests, which the copy leaves out.

A test file is one object with a `description` and a `tests` array of test cases.
Most files cover one keyword, such as `minimum.json`.
Files with the suffix `-legacy` cover keywords and behaviors that later dialects replaced, such as `id`.

A test case, which the tests of this module call a group, is an object with these members:

- `description` and an optional `comment`.
- `schema`, the schema under test. It holds no `$schema`, so it serves every dialect.
- `tests`, an array of tests. A test holds a `description`, the instance as `data`, and `valid`, whether the schema accepts the instance.
- `compatibility`, an optional string selecting the dialects the case applies to.
- `tags`, an optional list of behaviors the case expects of an implementation.
- `externalSchemas`, an optional object mapping retrieval URIs to the documents the schema refers to.
- `specification`, an optional list of the sections of the specification that define the behavior.

A JSON pointer to the schema of the case at index `i` of a file is `/tests/<i>/schema`.

### Compatibility

A dialect is spelled by its release number: 3, 4, 6, 7, 2019 or 2020.
Draft 2020-12 is 2020.
A missing `compatibility` admits every dialect.

The string is a comma-separated list of constraints ordered by release.
A bare number admits that release and later, `<=` admits that release and earlier, and `=` admits that release only.
The constraints are matched in order, and the first constraint whose release is at least the release under test decides.
After the last constraint, the last constraint decides.
For 2020, `7`, `2019,<=2020`, `<=3,7` and `=2020` admit the case, and `<=2019`, `=2019` and `9999` do not.
`9999` marks a feature of the specification that no release holds yet.

### Optional tests

Two markers make a test case optional for draft 2020-12:

- The file lies under `optional/`. The directory holds cases that the specification does not require, or that apply to some programming languages only.
- The case carries the tag `format-assertion`. Draft 2020-12 makes `format` an annotation by default, and an implementation may assert it instead. The cases of the tag sit in the top-level `format-*.json` files, which earlier versions of the suite held under `optional/format/`.

A case tagged `format-annotation` expects `format` to annotate only.
An implementation that asserts `format` skips it.

### External schemas

`externalSchemas` replaces the former `remotes/` directory.
Each key is the retrieval URI of a document, mostly under `http://localhost:1234/`, and the value is the document.
A document usually holds no `$id`, so its retrieval URI is its base URI.
The documents are scoped to their test case: two cases may map one URI to different documents.

## Refresh

Run this at the repository root:

```sh
just refresh-jsonschema-suite <commit-or-ref>
```

The recipe replaces the copied paths and rewrites the commit line above.
It rejects a ref that lacks one of the copied paths.
