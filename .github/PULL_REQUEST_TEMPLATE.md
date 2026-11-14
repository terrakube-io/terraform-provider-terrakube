<!-- The title becomes the commit message: type(scope): lowercase description. See CONTRIBUTING.md. -->

Closes #

## Changes

-

## Checks

- [ ] `gofmt -s -w .`, `go vet ./...` and `golangci-lint run` pass
- [ ] `go test ./...` passes, with tests for new and changed behaviour
- [ ] `go generate ./...` run, and `docs/` committed
- [ ] Example added or updated in `examples/`
- [ ] New API attributes are pointer types with `json:",omitempty"`
- [ ] Secrets are write-only or marked sensitive

## Breaking change

- [ ] This makes an existing plan fail or forces a replacement. The title has `!` and the description says what users must change.
