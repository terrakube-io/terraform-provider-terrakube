---
page_title: "Supported Terrakube versions"
subcategory: ""
description: |-
  Which Terrakube server version the provider is built for, and what it needs to build.
---

# Supported Terrakube versions

Terrakube can change its API in any release, and the provider is released separately.

| Provider version | Terrakube server |
|---|---|
| Releases after 0.33.1 | 2.34.1 |
| 0.30.0 to 0.33.1 | 2.34.0 |

Earlier releases did not record a server version.

The provider's acceptance tests run against the Terrakube version in the first row on every pull
request, and every week against the latest Terrakube release and the latest release of the
previous minor.

Releases after 0.33.1 are built with
[terrakube-go](https://github.com/terrakube-io/terrakube-go) v1.4.0, which targets Terrakube 2.34.1.
Resources move to it over time. Building the provider from source needs Go 1.27 or later.

Resources and attributes that need a newer server than the rest say so in their own
documentation, for example "(Terrakube 2.34.0+)". Attributes of that kind are optional and left
out of the request when unset, so a configuration that does not use them works on an older
server.

If you hit a problem on a combination that is not listed, please
[open an issue](https://github.com/terrakube-io/terraform-provider-terrakube/issues/new/choose)
with the provider and Terrakube versions.
