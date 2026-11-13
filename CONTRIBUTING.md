# Contributing

Code of conduct: [.github/CODE_OF_CONDUCT.md](.github/CODE_OF_CONDUCT.md).

## Setup

- [Go](https://go.dev/doc/install), the version in `go.mod`
- [Terraform](https://developer.hashicorp.com/terraform/downloads) 1.0+ (1.11+ or OpenTofu 1.11+
  for write-only `*_wo` arguments)

```sh
git clone https://github.com/terrakube-io/terraform-provider-terrakube.git
cd terraform-provider-terrakube
go build -v ./...
```

To use a local build, run `go install .` and add a
[`dev_overrides`](https://developer.hashicorp.com/terraform/cli/config/config-file#development-overrides-for-provider-developers)
block to the Terraform CLI configuration.

## Layout

- `internal/provider/<name>_resource.go`, `<name>_data_source.go`: schema and CRUD.
- `internal/provider/<name>_resource_test.go`, `<name>_data_source_test.go`: unit tests.
- `internal/provider/provider.go`: every resource and data source is registered here.
- `internal/client/`: API models. The Terrakube API is JSON:API, so models use `jsonapi` tags.
- `examples/`: one example per resource and data source.
- `templates/`: custom documentation. `docs/` is generated; do not edit it.

## Conventions

- A new API attribute that older Terrakube versions do not have is a pointer type with
  `json:",omitempty"`, so it is omitted from the request when unset.
- Unit tests mock the Terrakube API with `httptest.NewServer`. Cover new behaviour, and old
  behaviour when it changes.
- A secret value should not be stored in state. See `value_wo` on `terrakube_collection_item`
  and the variable resources.
- A change that makes an existing plan fail, or forces a replacement, is a breaking change.

## Checks

```sh
gofmt -s -w .        # CI fails on a diff
go vet ./...
golangci-lint run    # rules in .golangci.yml
go test ./...
go generate ./...    # regenerates docs/; CI fails if it changes any file
make testacc         # acceptance tests (TF_ACC=1), need a Terrakube instance
```

## Pull requests

Pull requests are squash-merged and the title becomes the commit message. The title uses the
[conventional commits](https://www.conventionalcommits.org) format with a lowercase
description. The `PR Title` check enforces it.

```
feat(vcs): add write-only client_secret_wo
fix(workspace): keep module_ssh_key on update
feat(variable)!: mark value as sensitive
```

Types: `feat`, `fix`, `perf`, `refactor`, `docs`, `test`, `build`, `ci`, `chore`, `revert`.
`!` after the type marks a breaking change. The description states what users must change.

`CHANGELOG.md` and release tags are generated. See [RELEASING.md](RELEASING.md).

## Issues

Open an [issue](https://github.com/terrakube-io/terraform-provider-terrakube/issues) with the
provider, Terrakube and Terraform or OpenTofu versions, a minimal configuration and the error.
Remove tokens and secrets first.
