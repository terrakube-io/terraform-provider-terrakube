# Contributing

Thanks for helping with the Terrakube Terraform provider. This page covers how to build and
test a change, and what a pull request needs to be merged and released. Please also read the
[Code of Conduct](.github/CODE_OF_CONDUCT.md).

## Getting started

You need [Go](https://go.dev/doc/install) (the version in `go.mod`) and
[Terraform](https://developer.hashicorp.com/terraform/downloads) 1.0 or later. Write-only
arguments (`*_wo`) need Terraform or OpenTofu 1.11 or later to try by hand.

```sh
git clone https://github.com/terrakube-io/terraform-provider-terrakube.git
cd terraform-provider-terrakube
go build -v ./...
```

To try the provider against a Terrakube instance, run `go install .` and point Terraform at it
with a [`dev_overrides`](https://developer.hashicorp.com/terraform/cli/config/config-file#development-overrides-for-provider-developers)
block in your CLI configuration.

## Making a change

Each resource and data source lives in `internal/provider/`:

- `<name>_resource.go` and `<name>_data_source.go` hold the schema and the CRUD methods.
- `<name>_resource_test.go` and `<name>_data_source_test.go` hold the unit tests.
- Every new resource or data source is registered in `internal/provider/provider.go`.
- API models are in `internal/client/`. The Terrakube API is JSON:API, so use the `jsonapi`
  struct tags.

Please follow these conventions:

- **Stay compatible with older Terrakube versions.** A new API attribute that older servers do
  not have must be a pointer type with `json:",omitempty"`, so it is left out of the request
  when unset.
- **Add tests.** Unit tests mock the Terrakube API with `httptest.NewServer`, so they run
  without a server. Cover the new behaviour, and the old behaviour if you change it.
- **Add an example** under `examples/` for a new resource or data source. Custom documentation
  goes in `templates/`, never straight into `docs/`, because `docs/` is generated.
- **Keep secrets out of state.** If a value is a secret, check whether a write-only argument
  fits. `value_wo` on `terrakube_collection_item` and the variables is the pattern to follow.

## Checks to run before opening a pull request

```sh
gofmt -s -w .          # formatting; CI fails on a diff
go vet ./...
golangci-lint run      # rules are in .golangci.yml
go test ./...          # unit tests
go generate ./...      # regenerates docs/; commit what it changes
```

Acceptance tests run against a real Terrakube instance and are skipped unless `TF_ACC=1` is
set. CI runs them on several Terraform versions:

```sh
make testacc
```

CI also fails if `go generate ./...` changes any file, so run it after any schema or
description change.

## Pull requests

- Keep a pull request to one change, and describe what it does and how you tested it.
- Say so in the description if a change can break existing configurations. Anything that
  makes a plan fail, or forces a resource to be replaced, is a breaking change.
- Pull requests are **squash-merged**, and the **title becomes the commit message**. The title
  must follow the [conventional commits](https://www.conventionalcommits.org) format, with a
  lowercase description:

  ```
  feat(vcs): add write-only client_secret_wo
  fix(workspace): keep module_ssh_key on update
  feat(variable)!: mark value as sensitive
  ```

  Use `feat`, `fix`, `perf`, `refactor`, `docs`, `test`, `build`, `ci`, `chore` or `revert`.
  Add a `!` after the type for a breaking change, and explain what users have to change in the
  description. A `PR Title` check enforces the format, and you can edit the title at any time.

Releases and the changelog are generated from these titles, so you never edit `CHANGELOG.md`
or tag a release yourself. [RELEASING.md](RELEASING.md) explains how releases work.

## Reporting bugs and asking for features

Open an [issue](https://github.com/terrakube-io/terraform-provider-terrakube/issues). For a
bug, include the provider version, the Terrakube version, the Terraform or OpenTofu version,
a minimal configuration and the error. Remove tokens and secrets from anything you paste.
