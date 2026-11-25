# Tasks spanning the Go modules of this repository.

modules := "kamaruntime kamalib kamacli"
suite_dir := "kamalib/pkg/jsonschema/testdata/suite"

default:
    @just --list

# Create or update a local go.work using every module. go.work is not
# committed, so each checkout creates its own.
work:
    test -f go.work || go work init
    go work use {{modules}}

# Tidy every module.
tidy:
    for m in {{modules}}; do (cd $m && go mod tidy); done

# Regenerate generated sources.
generate:
    cd kamalib && go run ./cmd/kama-simple && go generate ./...

build:
    for m in {{modules}}; do (cd $m && go build ./...); done

test:
    for m in {{modules}}; do (cd $m && go test ./...); done

# Format with the project's golangci-lint formatters.
fmt:
    for m in {{modules}}; do (cd $m && golangci-lint fmt ./...); done

lint:
    for m in {{modules}}; do (cd $m && golangci-lint run ./...); done

vet:
    for m in {{modules}}; do (cd $m && go vet -vettool="$(command -v ngcheckers)" ./...); done

check: fmt lint vet test

# Generate the JSON-Schema-Test-Suite groups as ignored fixtures and check them.
test-jsonschema-suite:
    cd kamalib && KAMA_JSONSCHEMA_SUITE=1 go test -count=1 -run TestSuiteBehavior ./generator/jsonschema/

# Refresh the vendored JSON-Schema-Test-Suite from a ref of upstream. The
# copy keeps validation/tests without proposals/, which holds only tests of
# unreleased keywords, the README and the schema of the test files of
# validation/, and LICENSE.
refresh-jsonschema-suite ref="main":
    #!/bin/sh
    set -eu
    tmp="$(mktemp -d)"
    trap 'rm -rf "$tmp"' EXIT
    git -C "$tmp" init -q
    git -C "$tmp" fetch -q --depth 1 https://github.com/json-schema-org/JSON-Schema-Test-Suite.git "{{ref}}"
    commit="$(git -C "$tmp" rev-parse FETCH_HEAD)"
    paths="validation/tests validation/README.md validation/validation-test-schema.json LICENSE"
    for p in $paths; do
        if ! git -C "$tmp" cat-file -e "$commit:$p" 2>/dev/null; then
            echo "$commit has no $p; upstream changed its layout" >&2
            exit 1
        fi
    done
    rm -rf "{{suite_dir}}/validation" "{{suite_dir}}/LICENSE"
    mkdir -p "{{suite_dir}}"
    git -C "$tmp" archive "$commit" $paths | tar -x -C "{{suite_dir}}" --exclude=validation/tests/proposals
    sed -i "s/^Commit: .*/Commit: \`$commit\`/" "{{suite_dir}}/README.md"
