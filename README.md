caddy-mwcache
========
[![go doc badge]][go doc link]
[![Github checks status]][github checks link]
[![codecov.io status]][codecov.io link]

caddy-mwcache is a cache plugin for [MediaWiki].

### Usage
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

```caddyfile
mwcache {
    ristretto {
        num_counters <value>
        max_cost <value>
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

### Configuring MediaWiki
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

### Build
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

### Development
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
[T264735]: https://phabricator.wikimedia.org/T264735
[localsettings.php]: https://www.mediawiki.org/wiki/Manual:LocalSettings.php
[xcaddy]: https://github.com/caddyserver/xcaddy
[docker-compose]: https://docs.docker.com/compose/

[GNU Affero General Public License v3.0]: LICENSE
[COPYRIGHT]: COPYRIGHT
