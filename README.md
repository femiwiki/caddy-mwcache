caddy-mwcache
========

### Prerequisites

- Go 1.15
- [xcaddy](https://github.com/caddyserver/xcaddy)

### Instructions

#### Build

```bash
xcaddy build

# ./caddy is the output
```

#### Development

Prerequisites:

- [docker-compose](https://docs.docker.com/compose/)

```bash
# Run a php-fpm server
docker-compose --project-directory example up --detach
# Run a web server
xcaddy start --config example/Caddyfile

# Test
curl -so /dev/null -w "%{time_total}\n" 127.0.0.1:2015

# Reload Caddyfile
xcaddy reload --config example/Caddyfile

# End
docker-compose --project-directory example down
xcaddy stop
```
