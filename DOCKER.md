# Vault deployment

中文使用、初始化、备份及部署说明见 [README.md](README.md)。

Vault runs an HTTP server; your gateway terminates TLS. The same image supports
root-path and subpath deployments through runtime configuration.

## Start

```bash
cp .env.example .env
docker compose build app
docker compose run --rm --no-deps app -generate-login-2fa-secret
```

The generator prints a new Base32 key without starting Vault. Add that key to
your phone authenticator (TOTP, SHA-1, 6 digits, 30 seconds), set
`DEVHUB_ADMIN_TOTP_SECRET` to it in `.env`, then run `docker compose up -d`.
Compose defaults to `DEVHUB_REQUIRE_LOGIN_2FA=1`: a missing or invalid key causes
startup to fail. Set the flag to `0` only to explicitly disable login 2FA.
The environment holds the long-lived secret, not a changing six-digit code.

- `http://localhost:8787/`: local maintenance and first administrator setup.
- `http://127.0.0.1:8788/`: public Vault upstream for a trusted HTTPS gateway.

Both host ports are bound to loopback. All Vault data stays in the existing
`devhub-data` volume, with the database at `/data/.vault.db`.

First create the `admin` account through port 8787. There is no default password.
The account password and Vault master password must each have at least 12
characters. The public listener refuses initial administrator creation.

This is a single-user application: only `admin` can sign in. Legacy member
records are retained but no longer authenticate, and user-management endpoints
have been removed. When 2FA is enabled, both listeners require a phone code at
login and the maintenance listener also requires one during initial setup.
Successful codes cannot be reused, including after a service restart.

The login secret stays in process configuration; it is not returned to the
browser or saved to the database. To rotate it or recover from a lost phone,
generate a new key, update your authenticator and `.env`, then recreate the
container with `docker compose up -d`. Account and master passwords are unchanged.
Native Web processes accept the same exported environment variables; without an
explicit `DEVHUB_REQUIRE_LOGIN_2FA=1`, native login 2FA defaults to disabled.

Project management has been removed. Existing `.devhub.db` files are left alone
but are no longer opened. There is no `/projects` mount or project launcher.

## Runtime URL prefix

The default is `/`. Prefer a dedicated hostname such as `vault.example.com`.
If your gateway shares one hostname between services, set this in `.env`:

```dotenv
DEVHUB_BASE_PATH=/vault/
```

Then apply the environment change without rebuilding:

```bash
docker compose up -d
```

The public upstream now serves `/vault/`, `/vault/assets/...`, and
`/vault/api/...`. The gateway must preserve `/vault`; remove any old strip-prefix
rule. `/vault` redirects to `/vault/`. Nested mounts such as `/tools/vault/`
also work. To return to root access, set `DEVHUB_BASE_PATH=/` and run the same
command. The local maintenance endpoint always stays at `/`.

The frontend is built with relative asset URLs. The HTTP server injects a base
element into the page, and API URLs and session cookie paths use that same
runtime prefix. This also keeps deep page refreshes under the correct mount.

For native execution, the equivalent flag is `-base-path /vault/`; it overrides
`DEVHUB_BASE_PATH` and applies to the optional `-remote-vault-port` listener.

## HTTPS gateway

Keep `DEVHUB_REQUIRE_HTTPS=1` for gateway deployments. It checks the original
browser scheme, not the HTTP hop from the gateway to the application. The
gateway must overwrite `X-Forwarded-Proto` and `X-Forwarded-For`, preserve the
public Host, and be the only remote party able to reach the public upstream.
Port 8787 does not trust forwarded headers and remains the local setup endpoint.

A root-path Caddy deployment needs only:

```caddy
vault.example.com {
    reverse_proxy 127.0.0.1:8788
}
```

Caddy sets the forwarding headers automatically. For a subpath, with
`DEVHUB_BASE_PATH=/vault/`, use `handle`, which preserves the path:

```caddy
example.com {
    handle /vault {
        redir /vault/ 308
    }
    handle /vault/* {
        reverse_proxy 127.0.0.1:8788
    }
}
```

With Nginx, place this inside your HTTPS server block:

```nginx
location = /vault {
    return 308 /vault/$is_args$args;
}
location /vault/ {
    # No trailing slash: preserve /vault/... when forwarding.
    proxy_pass http://127.0.0.1:8788;
    proxy_set_header Host $http_host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-For $remote_addr;
    client_max_body_size 16m;
}
```

If another trusted tunnel terminates TLS before Nginx, the inner Nginx HTTP hop
must forward the known external scheme `https` instead of `$scheme`. Restrict
that listener to the tunnel. A gateway running in another container should
connect to `app:8788` over a private Docker network, not its own loopback address.
Never route a public gateway to port 8787.

Local maintenance works over HTTP with no gateway. Direct HTTP on the public
listener is available for controlled private deployments with
`DEVHUB_REQUIRE_HTTPS=0`; browser access over that connection is unencrypted.

## Update and operate

Existing deployments need one source/image rebuild to adopt this refactor:

```bash
docker compose up -d --build
```

After that, prefix, timezone and host-port changes need only
`docker compose up -d`. Source changes still require rebuilding.

```bash
docker compose ps
docker compose logs -f app
docker compose down
```

`docker compose down` preserves the data volume. `docker compose down -v`
deletes it and must only be used when you intend to erase the database.
