# Changelog

## [1.0.1](https://github.com/femiwiki/caddy-mwcache/compare/v1.0.0...v1.0.1) (2026-10-10)


### Bug Fixes

* emit RFC-compliant Date headers ([#107](https://github.com/femiwiki/caddy-mwcache/issues/107)) ([e2cd34a](https://github.com/femiwiki/caddy-mwcache/commit/e2cd34a39926be398edc15b7e57233c86fa5b621))
* keep serving the response when a cache write is dropped ([#108](https://github.com/femiwiki/caddy-mwcache/issues/108)) ([d068dcb](https://github.com/femiwiki/caddy-mwcache/commit/d068dcb855c705abb66ca8cd01468c9a101a378b))
* require the ristretto options rather than starting without them ([#130](https://github.com/femiwiki/caddy-mwcache/issues/130)) ([034e3a2](https://github.com/femiwiki/caddy-mwcache/commit/034e3a24242d5d26daaa5330dedce6e976e3870e)), closes [#127](https://github.com/femiwiki/caddy-mwcache/issues/127) [#179](https://github.com/femiwiki/caddy-mwcache/issues/179)


### Performance Improvements

* compile the header regexps once ([#109](https://github.com/femiwiki/caddy-mwcache/issues/109)) ([6f1e9d8](https://github.com/femiwiki/caddy-mwcache/commit/6f1e9d8912b7bc4db449487ea51eb7647f151d58))
* share one buffer pool across requests ([#110](https://github.com/femiwiki/caddy-mwcache/issues/110)) ([92141f5](https://github.com/femiwiki/caddy-mwcache/commit/92141f5711b269f65d4e403200299bfe36fa7e97))

## [1.0.0](https://github.com/femiwiki/caddy-mwcache/compare/v0.3.0...v1.0.0) (2026-10-04)


### ⚠ BREAKING CHANGES

* a PURGE addressed to $wgInternalServer (127.0.0.1) no longer reaches entries stored under the public host. host_alias, in the next commit, maps it back.

### Features

* add a static block that caches static files by their version hash ([#160](https://github.com/femiwiki/caddy-mwcache/issues/160)) ([c38104e](https://github.com/femiwiki/caddy-mwcache/commit/c38104e85938f11d6475cc8007bef8ac2222f948)), closes [#159](https://github.com/femiwiki/caddy-mwcache/issues/159)
* put the request's host in the cache key ([#171](https://github.com/femiwiki/caddy-mwcache/issues/171)) ([4dffb41](https://github.com/femiwiki/caddy-mwcache/commit/4dffb41d2f71f4334a0ae5fe3691a9042c0844fd))
* serve load.php from the cache to requests with a session cookie ([#167](https://github.com/femiwiki/caddy-mwcache/issues/167)) ([485f578](https://github.com/femiwiki/caddy-mwcache/commit/485f5782fcdf8da87a7a12bab58022f2069485dc))
* store pages gzipped ([#164](https://github.com/femiwiki/caddy-mwcache/issues/164)) ([88764d1](https://github.com/femiwiki/caddy-mwcache/commit/88764d100aa9c829c001d1c4c99ee40ff0c3ff93))


### Bug Fixes

* never store a HEAD response ([#169](https://github.com/femiwiki/caddy-mwcache/issues/169)) ([59521cb](https://github.com/femiwiki/caddy-mwcache/commit/59521cb029a570a016c0a9c9eefa6c7f82a1b84e))
* write cached responses that have an empty body ([#166](https://github.com/femiwiki/caddy-mwcache/issues/166)) ([59f5bd6](https://github.com/femiwiki/caddy-mwcache/commit/59f5bd602d7269304a5277a6a2c4d72910948ac4))

## [0.3.0](https://github.com/femiwiki/caddy-mwcache/compare/v0.2.0...v0.3.0) (2026-09-25)


### Features

* add max_cost_bytes, a budget spent in bytes ([#141](https://github.com/femiwiki/caddy-mwcache/issues/141)) ([cd21b0f](https://github.com/femiwiki/caddy-mwcache/commit/cd21b0ff613f928ab3787e617131d4ecf915c608))

## [0.2.0](https://github.com/femiwiki/caddy-mwcache/compare/v0.1.2...v0.2.0) (2026-09-25)


### Features

* report the cache's counters as Caddy metrics ([#139](https://github.com/femiwiki/caddy-mwcache/issues/139)) ([cbdb904](https://github.com/femiwiki/caddy-mwcache/commit/cbdb9044d0a39a16a285a9e13eadef5442d685bf))

## [0.1.2](https://github.com/femiwiki/caddy-mwcache/compare/v0.1.1...v0.1.2) (2026-09-21)


### Bug Fixes

* load the config from the handler instead of a package variable ([#126](https://github.com/femiwiki/caddy-mwcache/issues/126)) ([3762f29](https://github.com/femiwiki/caddy-mwcache/commit/3762f29b35fb90fa7eaa76adef39cdfdd2648a3b)), closes [#119](https://github.com/femiwiki/caddy-mwcache/issues/119)

## [0.1.1](https://github.com/femiwiki/caddy-mwcache/compare/v0.1.0...v0.1.1) (2026-09-20)


### Bug Fixes

* stop returning EOF after an uncacheable response ([#122](https://github.com/femiwiki/caddy-mwcache/issues/122)) ([7ce0491](https://github.com/femiwiki/caddy-mwcache/commit/7ce049190416f6654d7e51daf4d8aa0693408802)), closes [#118](https://github.com/femiwiki/caddy-mwcache/issues/118)

## v0.1.0 - 2026-07-01

- Drops supports for 'map' and 'badger' backend.
- Requires Go 1.25.
- Changed external libraries:
  - Bump caddy from 2.9.1 to 2.11.4
  - Bump ristretto from 0.1.1 to 0.2.0 (#74)
  - Bump go-strcase from 1.3.0 to 1.3.1 (#87)

## v0.0.4

- Does not cache 304.
- Reverts "Does not serve cache if the body is binaries" and "Does not cache binary responses".

## v0.0.3

- Does not serve cache if the body is binaries.
- Changed external libraries:
  - Bump ristretto from 0.0.4 to 0.1.0 (#11)
  - Bump badger from 3.2011.1 to 3.2103.0
  - Bump caddy from 2.3.0 to 2.4.1

## v0.0.2

- Does not cache binary responses.

## v0.0.1

- Adds support for [ristretto](https://github.com/dgraph-io/ristretto) as backend. The default backend is now changed to ristretto.
- Adds Caddyfile directives for configuring BadgerDB. The in-memory mode is not default now.
- Does not cache <200 or 400>= status response
