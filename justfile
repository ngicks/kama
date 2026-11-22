# Tasks spanning the Go modules of this repository.

modules := "kamalib kamacli"

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
