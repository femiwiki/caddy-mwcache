# caddy-mwcache [![Github checks status]][github checks link] [![codecov.io status]][codecov.io link]

**⚠️ Work-in-progress** - See [milestones for v1.0.0](https://github.com/femiwiki/caddy-mwcache/milestone/1)

caddy-mwcache is a cache plugin for [MediaWiki].

### Usage

**NOTE**: You cannot use this plugin if the next conditions match:

- MediaWiki < v1.36
- php-curl extension installed
- curl >= v7.62

See https://phabricator.wikimedia.org/T264735 for further details.

```caddyfile
mwcache {
  [<backend>]
  [purge_acl 127.0.0.1]
  [purge_acl {
    <address1>
    <...>
  }]
```

- **backend** is either `map`(experimental), or `badger`. Default to `badger`.
- **purge_acl** is either a list of acl or a ip address that are allowed to request to purge cache.

You must add the next lines your [LocalSettings.php].

```php
$wgUseCdn = true;
$wgCdnServers = '127.0.0.1';
```

### Build

Prerequisites:

- Go 1.15
- [xcaddy]

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
- [xcaddy]
- [docker-compose]

```bash
# Start a php-fpm server
docker-compose --project-directory example up --detach
# Start a web server
xcaddy start --config example/Caddyfile

# Test
curl -so /dev/null -w "%{time_total}\n" '127.0.0.1:2015'

# Reload Caddyfile
xcaddy reload --config example/Caddyfile

# Stop
docker-compose --project-directory example down
xcaddy stop
```

[github checks status]: https://badgen.net/github/checks/femiwiki/caddy-mwcache
[github checks link]: https://github.com/femiwiki/caddy-mwcache/actions
[codecov.io status]: https://badgen.net/codecov/c/github/femiwiki/caddy-mwcache
[codecov.io link]: https://codecov.io/gh/femiwiki/caddy-mwcache
[mediawiki]: https://www.mediawiki.org
[xcaddy]: https://github.com/caddyserver/xcaddy
[docker-compose]: https://docs.docker.com/compose/
[localsettings.php]: https://www.mediawiki.org/wiki/Manual:LocalSettings.php
