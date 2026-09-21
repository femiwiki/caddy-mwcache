# Changelog

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
