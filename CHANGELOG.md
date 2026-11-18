# Changelog

## [0.35.0](https://github.com/terrakube-io/terraform-provider-terrakube/compare/v0.34.0...v0.35.0) (2026-11-18)


### Features

* **registry:** add provider version and implementation resources ([#259](https://github.com/terrakube-io/terraform-provider-terrakube/issues/259)) ([64eaa90](https://github.com/terrakube-io/terraform-provider-terrakube/commit/64eaa901f84a60586b07afad04432174026fd89e))
* **registry:** add terrakube_registry_provider resource and data source ([#258](https://github.com/terrakube-io/terraform-provider-terrakube/issues/258)) ([b32aeb5](https://github.com/terrakube-io/terraform-provider-terrakube/commit/b32aeb5b4367024d905fb59a34002288d2724386))


### Bug Fixes

* **provider:** mark the token attribute as sensitive ([#260](https://github.com/terrakube-io/terraform-provider-terrakube/issues/260)) ([d4f9882](https://github.com/terrakube-io/terraform-provider-terrakube/commit/d4f9882e382d2f461f60d0c20640d915a95bb346))

## [0.34.0](https://github.com/terrakube-io/terraform-provider-terrakube/compare/v0.33.1...v0.34.0) (2026-11-18)


### Features

* **workspace_run_trigger:** add terrakube_workspace_run_trigger data source ([#263](https://github.com/terrakube-io/terraform-provider-terrakube/issues/263)) ([1623f33](https://github.com/terrakube-io/terraform-provider-terrakube/commit/1623f33a4265139010c011a89cb9d8c137da4462))
* **workspace_schedule:** add terrakube_workspace_schedule data source ([#264](https://github.com/terrakube-io/terraform-provider-terrakube/issues/264)) ([492b997](https://github.com/terrakube-io/terraform-provider-terrakube/commit/492b99727ffa8cf2a3662c366e82414b0adf17b0))
* **workspace_schedule:** validate the cron expression at plan time ([#253](https://github.com/terrakube-io/terraform-provider-terrakube/issues/253)) ([225fb69](https://github.com/terrakube-io/terraform-provider-terrakube/commit/225fb69bffbe5c38b5b0a9c093b130fd41e352cf))


### Bug Fixes

* report failed deletes in every resource, and finish the resource tests ([#257](https://github.com/terrakube-io/terraform-provider-terrakube/issues/257)) ([b038b70](https://github.com/terrakube-io/terraform-provider-terrakube/commit/b038b70398bc7faa8c604e058af7b36a0d770f2b))
* report failed deletes, and test six more resources ([#255](https://github.com/terrakube-io/terraform-provider-terrakube/issues/255)) ([9ae7f12](https://github.com/terrakube-io/terraform-provider-terrakube/commit/9ae7f1296e101cff297666a7bbcd0b37a408997f))


### Code Refactoring

* **provider:** add a shared terrakube-go client ([#269](https://github.com/terrakube-io/terraform-provider-terrakube/issues/269)) ([2969aa9](https://github.com/terrakube-io/terraform-provider-terrakube/commit/2969aa9d5864865207a606cd99c6302a0cb21cd4))

## [0.33.1](https://github.com/terrakube-io/terraform-provider-terrakube/compare/v0.33.0...v0.33.1) (2026-11-14)


### Bug Fixes

* **notification:** clear the signing secret when it is removed ([#241](https://github.com/terrakube-io/terraform-provider-terrakube/issues/241)) ([6352a07](https://github.com/terrakube-io/terraform-provider-terrakube/commit/6352a0756460e9373889ae11da73acdecaf02d37))

## [0.33.0](https://github.com/terrakube-io/terraform-provider-terrakube/compare/v0.32.1...v0.33.0) (2026-11-14)


### Features

* **access:** add project_access and workspace_access data sources ([#237](https://github.com/terrakube-io/terraform-provider-terrakube/issues/237)) ([510716c](https://github.com/terrakube-io/terraform-provider-terrakube/commit/510716cf22d946395bae82ad72d9e08ee2794bca))
* **collection:** add collection and collection_item data sources ([#235](https://github.com/terrakube-io/terraform-provider-terrakube/issues/235)) ([7fe28e7](https://github.com/terrakube-io/terraform-provider-terrakube/commit/7fe28e717a4f65413912a9bf880fc6ad0e1c052a))
* **module:** add terrakube_module data source ([#231](https://github.com/terrakube-io/terraform-provider-terrakube/issues/231)) ([2bdc785](https://github.com/terrakube-io/terraform-provider-terrakube/commit/2bdc785b017580c79315208395adf699b7c082e4))
* **variable:** add organization_variable and workspace_variable data sources ([#232](https://github.com/terrakube-io/terraform-provider-terrakube/issues/232)) ([e30b5dc](https://github.com/terrakube-io/terraform-provider-terrakube/commit/e30b5dcd5a06922daf09cc836b93114bc99ca93c))


### Bug Fixes

* **data-source:** require exactly one match in project, team and federated_credential ([#239](https://github.com/terrakube-io/terraform-provider-terrakube/issues/239)) ([4dd54f3](https://github.com/terrakube-io/terraform-provider-terrakube/commit/4dd54f3318dadae2f68248f0925b1fc23651c95e))
* **data-source:** return an error when a lookup finds nothing ([#238](https://github.com/terrakube-io/terraform-provider-terrakube/issues/238)) ([5de3700](https://github.com/terrakube-io/terraform-provider-terrakube/commit/5de37008eeb10de2161e4b6bfba1a79e1dd4c8c0))
* **workspace_schedule:** make import work with workspace_id,id ([#230](https://github.com/terrakube-io/terraform-provider-terrakube/issues/230)) ([ab3292b](https://github.com/terrakube-io/terraform-provider-terrakube/commit/ab3292bea8533cc14fda68a7e3200ac461dfe0d9))
* **workspace-tag:** escape backslashes in rsqlQuote ([#236](https://github.com/terrakube-io/terraform-provider-terrakube/issues/236)) ([7398c94](https://github.com/terrakube-io/terraform-provider-terrakube/commit/7398c9475b6fff7822c5c523b7d8f466cd169ee5))

## [0.32.1](https://github.com/terrakube-io/terraform-provider-terrakube/compare/v0.32.0...v0.32.1) (2026-11-14)


### Bug Fixes

* **ci:** quote label colors so label-sync reads them as strings ([#219](https://github.com/terrakube-io/terraform-provider-terrakube/issues/219)) ([03cb796](https://github.com/terrakube-io/terraform-provider-terrakube/commit/03cb796bb333e331e284fd942282e278afe8226e))
* **ci:** quote label descriptions that contain commas ([#225](https://github.com/terrakube-io/terraform-provider-terrakube/issues/225)) ([2502f0a](https://github.com/terrakube-io/terraform-provider-terrakube/commit/2502f0a0cd178cc8312db64cfa0ac5ee1954d488))
* **deps:** update Go and modules to clear govulncheck findings ([#215](https://github.com/terrakube-io/terraform-provider-terrakube/issues/215)) ([ee6ea7e](https://github.com/terrakube-io/terraform-provider-terrakube/commit/ee6ea7e59060fa25288b290380324c31e5e5adae))

## [0.32.0](https://github.com/terrakube-io/terraform-provider-terrakube/compare/v0.31.0...v0.32.0) (2026-11-13)


### ⚠ BREAKING CHANGES

* **variable:** mark value as sensitive on workspace and organization variables ([#192](https://github.com/terrakube-io/terraform-provider-terrakube/issues/192))

### Features

* **notification:** add write-only signing_secret_wo ([#199](https://github.com/terrakube-io/terraform-provider-terrakube/issues/199)) ([f2b2e74](https://github.com/terrakube-io/terraform-provider-terrakube/commit/f2b2e74b16e3a7084e8963511718d0a41491381f))
* **ssh:** add write-only private_key_wo ([#198](https://github.com/terrakube-io/terraform-provider-terrakube/issues/198)) ([11f9bec](https://github.com/terrakube-io/terraform-provider-terrakube/commit/11f9beca78ba1bc5977e2a441c578812e0334025))
* **variable:** add write-only value_wo to workspace and organization variables ([#183](https://github.com/terrakube-io/terraform-provider-terrakube/issues/183)) ([30e302d](https://github.com/terrakube-io/terraform-provider-terrakube/commit/30e302da1370ae2b24b23d552cb0056b03073c24))
* **vcs:** add write-only client_secret_wo and private_key_wo ([#193](https://github.com/terrakube-io/terraform-provider-terrakube/issues/193)) ([be50c8e](https://github.com/terrakube-io/terraform-provider-terrakube/commit/be50c8e23fbab206962bf0bdaab6a983903a3c9c))


### Bug Fixes

* **notification:** keep signing_secret, which Terrakube never returns ([#197](https://github.com/terrakube-io/terraform-provider-terrakube/issues/197)) ([77e85dd](https://github.com/terrakube-io/terraform-provider-terrakube/commit/77e85ddd85c2b98f76a527aee49b082394f71f39)), closes [#196](https://github.com/terrakube-io/terraform-provider-terrakube/issues/196)
* **variable:** mark value as sensitive on workspace and organization variables ([#192](https://github.com/terrakube-io/terraform-provider-terrakube/issues/192)) ([cc03f60](https://github.com/terrakube-io/terraform-provider-terrakube/commit/cc03f600c478e06b10bc1b2601c129f89bbc3e2b)), closes [#189](https://github.com/terrakube-io/terraform-provider-terrakube/issues/189)

## Changelog

Generated by [release-please](https://github.com/googleapis/release-please). See
[RELEASING.md](RELEASING.md).

Releases up to 0.31.0 are on the
[GitHub releases page](https://github.com/terrakube-io/terraform-provider-terrakube/releases).
