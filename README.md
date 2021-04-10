# caddy-mwcache

**⚠️ Work-in-progress**

caddy-mwcache is a cache plugin for [MediaWiki].

### Todo list

- [x] Handle caddyfile directive
- [x] Store cache to the backend
- [x] Response using the cache
- [x] Do not cache redirects
- [ ] Handle rewrite directive
- [ ] Provide purge acl directive
- Backend support
  - [x] map (Golang type)
  - [x] [badger](https://github.com/dgraph-io/badger)
  - [ ] [memcached](https://memcached.org/)
- [x] Handle PURGE request ([link](https://www.mediawiki.org/wiki/Manual:$wgCdnServers))
- [x] Don't cache authorized requests ([link](https://github.com/wikimedia/puppet/blob/120dff458fea24318bbcb31b457b5b7d113e66a9/modules/varnish/templates/misc-frontend.inc.vcl.erb#L36-L39))
- [ ] Don't cache cookie requests ([link](https://github.com/wikimedia/puppet/blob/120dff458fea24318bbcb31b457b5b7d113e66a9/modules/varnish/templates/misc-frontend.inc.vcl.erb#L41-L49))
- [ ] Distributed cache

### Usage

```caddyfile
mwcache [<backend>]
```

- **backend** is either `map`, or `badger`. Default to `badger`.

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
[docker-compose]: (https://docs.docker.com/compose/)
