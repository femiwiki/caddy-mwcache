# caddy-mwcache [![go doc badge]][go doc link] [![Github checks status]][github checks link] [![codecov.io status]][codecov.io link]

**⚠️ Work-in-progress** - See [milestones for v1.0.0](https://github.com/femiwiki/caddy-mwcache/milestone/1)

caddy-mwcache is a cache plugin for [MediaWiki].

### Usage

**NOTE**: You cannot use this plugin if the next conditions match:

- php-curl extension installed
- curl >= v7.62

See https://phabricator.wikimedia.org/T264735 for further details.

```caddyfile
mwcache {
  [<backend>]
  [ristretto {
    num_counters <value>
	  max_cost <value>
	  buffer_items <value>
    <additional config key1> <value1>
    <additional config key2> <value2>
  }]
  [badger {
    <badger option key1> <badger option value1>
    <badger option key2> <badger option value2>
  }]
  [purge_acl 127.0.0.1]
  [purge_acl {
    <address1>
    <...>
  }]
```

- **backend** is either `ristretto`, `badger`, or `map`(experimental). Default to `ristretto`.
- **ristretto** and **badger** are also used as a block to configure backend. Configuration keys are snake case versions of fields of [Ristretto's Config struct](https://pkg.go.dev/github.com/dgraph-io/ristretto#Config) or [Badger's Options struct](https://pkg.go.dev/github.com/dgraph-io/badger/v3@v3.2011.1#Options). But it is limited to only primitive types(bool, int, string).
- **purge_acl** is either a list of acl or a ip address that are allowed to request to purge cache.

You must add the next lines your [LocalSettings.php].

```php
$wgUseCdn = true;
$wgCdnServers = '127.0.0.1';
// If your web server supports TLS
$wgInternalServer = 'http://127.0.0.1';
```

### Build

Prerequisites:

- Go 1.15
- [xcaddy]

```bash
xcaddy build

# ./caddy is the output
```

### Development

Prerequisites:

- Go 1.15
- [docker-compose]

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

---

The source code of _femiwiki/caddy-mwcache_ is primarily distributed under the terms
of the [GNU Affero General Public License v3.0] or any later version. See
[COPYRIGHT] for details.

[go doc badge]: https://img.shields.io/badge/godoc-reference-blue.svg
[go doc link]: http://godoc.org/github.com/femiwiki/caddy-mwcache
[github checks status]: https://badgen.net/github/checks/femiwiki/caddy-mwcache/main
[github checks link]: https://github.com/femiwiki/caddy-mwcache/actions
[codecov.io status]: https://badgen.net/codecov/c/github/femiwiki/caddy-mwcache
[codecov.io link]: https://codecov.io/gh/femiwiki/caddy-mwcache
[mediawiki]: https://www.mediawiki.org
[xcaddy]: https://github.com/caddyserver/xcaddy
[docker-compose]: https://docs.docker.com/compose/
[localsettings.php]: https://www.mediawiki.org/wiki/Manual:LocalSettings.php
[copyright]: COPYRIGHT
