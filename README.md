# caddy-mwcache

[![go doc badge]][go doc link]
[![Github checks status]][github checks link]
[![codecov.io status]][codecov.io link]

caddy-mwcache is a cache plugin for [MediaWiki].

## Usage

```caddyfile
example.com {
    mwcache
}
```

Currently, only "ristretto" backend is supported and used by default.

```caddyfile
# Default value
mwcache {
    ristretto
    purge_acl 127.0.0.1
}
```

- **ristretto** is also used as a block to configure backend. Configuration keys
  are snake case versions of fields of [Ristretto's Config struct]. But it is
  limited to only primitive types(bool, int, string, etc).
- **purge_acl** is either a single item or a list of CIDRs or IP addresses that
  are allowed to request to purge cache.
- **max_cost_bytes** spends ristretto's `MaxCost` in bytes, by costing an entry
  at the size of what is stored. **max_cost** keeps its own meaning, a count of
  entries, since an entry costs 1 under it; saying both is an error. ristretto
  refuses an entry whose cost is above the whole budget, so a byte budget below
  one response caches nothing. `num_counters` is a count either way, and wants
  to be roughly ten times the entries the cache holds.

Pages are stored gzipped, so what is stored is the compressed size. A client
that accepts gzip gets the stored bytes as they are, which `encode` passes
through untouched; any other client gets them decompressed.

```caddyfile
mwcache {
    ristretto {
        num_counters <value>
        max_cost <value>
        max_cost_bytes <value>
        buffer_items <value>
        <additional config key1> <value1>
        <additional config key2> <value2>
    }
    purge_acl {
        <cidr1>
        <cidr2>
        <address1>
        <address2>
    }
}
```

## Static files

A `static` block sets `Cache-Control` on files `file_server` reads off disk,
the way Wikimedia's [static.php] does. MediaWiki links a file under
`resources/`, `skins/` and `extensions/` with the first five hex digits of its
md5 as the query, such as `icon.svg?6a22e`.

- The query is the file's hash: a year, `immutable`.
- The query looks like a hash but is not this file's: a minute. This is the URL
  a new stylesheet asks for while an older server is still answering.
- Anything else: `unversioned_max_age`.

Only 2xx and 304 responses from a regular file are touched, never a `.php`
file, and the page cache does not store them. Without the block nothing
changes.

```caddyfile
mwcache {
    static {
        # Defaults, as static.php has them
        paths /resources/* /skins/* /extensions/*
        versioned_max_age 365d
        unversioned_max_age 365d
        mismatch_max_age 1m
    }
}
```

## Configuring MediaWiki

> **WARNING**: If you are using php-curl extension with curl ≥7.62, you cannot
> use this plugin due to MediaWiki's bug [T264735].

You must add the next lines your [LocalSettings.php].

```php
// LocalSettings.php
$wgUseCdn = true;
$wgCdnServers = '127.0.0.1';
// If your web server supports TLS
$wgInternalServer = 'http://127.0.0.1';
```

## Build

Prerequisites:

- Go
- [xcaddy]

```bash
# Run the program right away
xcaddy
xcaddy version
xcaddy list-modules

# Build the binary, "./caddy" is the output
xcaddy build \
  --with github.com/femiwiki/caddy-mwcache
```

## Development

Use [docker-compose] to setup test environment.

```bash
# Start a php-fpm server
docker-compose --project-directory example up --detach
# Start a web server
docker-compose --project-directory example exec --workdir=/root/src caddy xcaddy start --config example/Caddyfile
# Or detach by run command
# docker-compose --project-directory example exec --workdir=/root/src caddy xcaddy run --config example/Caddyfile

# Test
curl -so /dev/null -w "%{time_total}\n" 'http://127.0.0.1:2015'
curl -so /dev/null -w "%{time_total}\n" 'http://127.0.0.1:2015/slow.php'
curl -so /dev/null -w "%{time_total}\n" 'http://127.0.0.1:2015/slow.php'

# Reload Caddyfile
docker-compose --project-directory example exec --workdir=/root/src caddy xcaddy reload --config example/Caddyfile

# Stop the web server
docker-compose --project-directory example exec --workdir=/root/src caddy xcaddy stop

# Stop the all services
docker-compose --project-directory example down
```

&nbsp;

--------

The source code of *femiwiki/caddy-mwcache* is primarily distributed under the
terms of the [GNU Affero General Public License v3.0] or any later version. See
[COPYRIGHT] for details.

[go doc badge]: https://img.shields.io/badge/godoc-reference-blue.svg
[go doc link]: http://godoc.org/github.com/femiwiki/caddy-mwcache
[github checks status]: https://badgen.net/github/checks/femiwiki/caddy-mwcache
[github checks link]: https://github.com/femiwiki/caddy-mwcache/actions
[codecov.io status]: https://badgen.net/codecov/c/github/femiwiki/caddy-mwcache
[codecov.io link]: https://codecov.io/gh/femiwiki/caddy-mwcache

[mediawiki]: https://www.mediawiki.org
[Ristretto's Config struct]: https://pkg.go.dev/github.com/dgraph-io/ristretto#Config
[static.php]: https://github.com/wikimedia/operations-mediawiki-config/blob/63f500d0e9d7a01855395347c8b10c5ea9bcd90f/w/static.php
[T264735]: https://phabricator.wikimedia.org/T264735
[localsettings.php]: https://www.mediawiki.org/wiki/Manual:LocalSettings.php
[xcaddy]: https://github.com/caddyserver/xcaddy
[docker-compose]: https://docs.docker.com/compose/

[GNU Affero General Public License v3.0]: LICENSE
[COPYRIGHT]: COPYRIGHT
