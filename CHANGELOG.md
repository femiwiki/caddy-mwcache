# Changelog

## Unreleased

- Do not serve cached binaries.

## v0.0.2

- Do not cache binary responses.

## v0.0.1

- Add support for [ristretto](https://github.com/dgraph-io/ristretto) as backend. The default backend is now changed to ristretto.
- Add Caddyfile directives for configuring BadgerDB. The in-memory mode is not default now.
- Do not cache <200 or 400>= status response
