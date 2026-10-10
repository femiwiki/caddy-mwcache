# caddy-mwcache

[![go doc badge]][go doc link]
[![Github checks status]][github checks link]
[![codecov.io status]][codecov.io link]

caddy-mwcache is a cache plugin for [MediaWiki].

## Usage

```caddyfile
example.com {
    mwcache {
        ristretto {
            num_counters 100000
            max_cost 10000
            buffer_items 64
        }
    }
}
```

Currently, only "ristretto" backend is supported and used by default. It has no
default size, so the ristretto block must set `num_counters`, `buffer_items`
and one of `max_cost` or `max_cost_bytes`.

- **ristretto** is also used as a block to configure backend. Configuration keys
  are snake case versions of fields of [Ristretto's Config struct]. But it is
  limited to only primitive types(bool, int, string, etc).
- **purge_acl** is either a single item or a list of CIDRs or IP addresses that
  are allowed to request to purge cache. It defaults to `127.0.0.1`.
- **max_cost_bytes** spends ristretto's `MaxCost` in bytes, by costing an entry
  at the size of what is stored. **max_cost** keeps its own meaning, a count of
  entries, since an entry costs 1 under it; saying both is an error. ristretto
  refuses an entry whose cost is above the whole budget, so a byte budget below
  one response caches nothing. `num_counters` is a count either way, and wants
  to be roughly ten times the entries the cache holds.
- **host_alias** `<host> <alias>...` makes requests and purges for each alias
  read, fill and delete the entries of `<host>`, see [Hosts](#hosts). It may be
  given more than once.

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
    host_alias <host> <alias1> <alias2>
}
```

## Hosts

The cache key is the host and the URI, as Wikimedia's Varnish keys them, so two
hosts behind one handler never share an entry, and a purge deletes only the
entry of the host it names. The host is lowercased and loses its port and any
trailing dot.

MediaWiki sends its purges to `$wgInternalServer`, so their host is that one,
not the one readers use. Make it an alias of `$wgServer`'s host. A second name
that serves the same pages, such as `www`, can be an alias too, and then shares
the entries instead of filling its own; a host that is not an alias of
`$wgServer`'s is never purged, and its entries live until they expire.

```caddyfile
example.com www.example.com 127.0.0.1:80 {
    mwcache {
        host_alias example.com www.example.com 127.0.0.1
    }
}
```

## Logged-in requests

A request with a session or token cookie (`([sS]ession|Token)=`) bypasses the
cache, except for `/load.php`. load.php defines `MW_NO_SESSION`, so the cookie
cannot change what it sends, and such a request reads and fills the same entry
as an anonymous one. A load.php request with a `user` parameter always
bypasses the cache. Only `/load.php` exactly counts, which is load.php with
`$wgScriptPath = ""`.

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

Then add `host_alias <$wgServer's host> 127.0.0.1` to `mwcache`; see
[Hosts](#hosts).

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
[Ristretto's Config struct]: https://pkg.go.dev/github.com/dgraph-io/ristretto/v2#Config
[static.php]: https://github.com/wikimedia/operations-mediawiki-config/blob/63f500d0e9d7a01855395347c8b10c5ea9bcd90f/w/static.php
[T264735]: https://phabricator.wikimedia.org/T264735
[localsettings.php]: https://www.mediawiki.org/wiki/Manual:LocalSettings.php
[xcaddy]: https://github.com/caddyserver/xcaddy
[docker-compose]: https://docs.docker.com/compose/

[GNU Affero General Public License v3.0]: LICENSE
[COPYRIGHT]: COPYRIGHT
