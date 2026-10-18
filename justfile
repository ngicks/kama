# Tasks spanning the Go modules listed in go.work.

modules := "kama cli proto"

default:
    @just --list

# Tidy every module.
tidy:
    for m in {{modules}}; do (cd $m && go mod tidy); done
    go work sync

# Regenerate generated sources.
generate:
    cd kama && go generate ./...

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
