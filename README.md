# caddy-mwcache

**⚠️ Work-in-progress**

caddy-mwcache is a cache plugin for [MediaWiki].

### Todo list

- [x] Handle caddyfile directive
- [x] Store cache to the backend
- [x] Response using the cache
- [x] Do not cache redirects
- [x] Cache headers
- [x] Don't disturb logged-in activities
- [x] Provide purge acl directive
- [ ] Support `Cache-Control` haeader directives (s-maxage and max-age etc) and `Expires:` header
- Backend support
  - [x] map (Golang type)
  - [x] [badger](https://github.com/dgraph-io/badger)
  - [ ] [memcached](https://memcached.org/)
- [x] Handle PURGE request ([link](https://www.mediawiki.org/wiki/Manual:$wgCdnServers))
- [x] Don't cache authorized requests ([link](https://github.com/wikimedia/puppet/blob/120dff458fea24318bbcb31b457b5b7d113e66a9/modules/varnish/templates/misc-frontend.inc.vcl.erb#L36-L39))
- [ ] Don't cache cookie requests ([link](https://github.com/wikimedia/puppet/blob/120dff458fea24318bbcb31b457b5b7d113e66a9/modules/varnish/templates/misc-frontend.inc.vcl.erb#L41-L49))
- [ ] Distributed cache

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

- **backend** is either `map`, or `badger`. Default to `badger`.
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

[mediawiki]: https://www.mediawiki.org
[xcaddy]: https://github.com/caddyserver/xcaddy
[docker-compose]: https://docs.docker.com/compose/
[localsettings.php]: https://www.mediawiki.org/wiki/Manual:LocalSettings.php
