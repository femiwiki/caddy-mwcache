# Changelog

## Unreleased

- Add support for [ristretto](https://github.com/dgraph-io/ristretto) as backend. The default backend is now  changed to ristretto.
- Add Caddyfile directives for configuring BadgerDB. The in-memory mode is not default now.
- Do not cache <200 or 400>= status response
