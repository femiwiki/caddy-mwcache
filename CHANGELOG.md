# Changelog

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
