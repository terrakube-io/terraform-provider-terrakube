---
page_title: "Supported Terrakube versions"
subcategory: ""
description: |-
  Which Terrakube server version the provider is built for.
---

# Supported Terrakube versions

Terrakube can change its API in any release, and the provider is released separately.

| Provider version | Terrakube server |
|---|---|
| 0.30.0 and later | 2.34.0 |

Earlier releases did not record a server version.

Resources and attributes that need a newer server than the rest say so in their own
documentation, for example "(Terrakube 2.34.0+)". Attributes of that kind are optional and left
out of the request when unset, so a configuration that does not use them works on an older
server.

If you hit a problem on a combination that is not listed, please
[open an issue](https://github.com/terrakube-io/terraform-provider-terrakube/issues/new/choose)
with the provider and Terrakube versions.
