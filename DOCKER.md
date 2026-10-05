# OrbStack / Docker deployment

The container runs DevHub's browser interface and stores its SQLite databases
in a named Docker volume. The published port is restricted to the Mac loopback
interface, so it is not exposed to the local network.

## Start with OrbStack

Open Terminal in this repository and run:

```bash
docker compose up -d --build
```

The two access points are deliberately separated:

- On the Mac, open <http://localhost:8787> for Vault and Workspace.
- On the Mac, port `8788` is reserved for a gateway or reverse proxy that
  exposes Vault-only access to other devices.

The Vault-only server does not advertise Workspace in the UI, and its backend
rejects every project-management API even if a client calls one manually.

By default, the repository's parent directory is mounted read-only at
`/projects`. To mount another macOS project directory, create `.env` first:

```bash
cp .env.example .env
```

Edit `DEVHUB_PROJECTS_DIR` to an absolute macOS path, then rebuild:

```bash
docker compose up -d --build
```

When adding a Workspace project, enter its container path. For example, a Mac
folder mounted from `/Users/alice/Projects` is entered as
`/projects/project-name`.

## Common commands

```bash
# View status and logs
docker compose ps
docker compose logs -f app

# Rebuild after updating the source
docker compose up -d --build

# Stop without deleting password or Workspace data
docker compose down
```

The persistent databases live in the `devhub-data` Docker volume. Do not run
`docker compose down -v` unless you intentionally want to delete that volume.

## Container limitation

The container cannot launch Finder, VS Code, Sublime Text, or Terminal on the
macOS host. Workspace records and mounted project paths still work, but the
Open menu is only able to launch applications when DevHub runs directly on the
host operating system.

## Public and mobile access

The browser API now has account login, server-side sessions, and a separate
vault-master-password unlock step. The initial setup creates the fixed super
administrator account `admin`; it has **no default password**. Choose a unique
admin account password and a separate vault master password (both at least 12
characters). The `admin` account can add and remove member accounts from the
Vault sidebar.

Complete the first `admin` setup only at <http://localhost:8787>. The public
Vault-only port deliberately refuses initial administrator creation, preventing
an internet visitor from claiming an uninitialized deployment.

DevHub does not hold TLS certificates itself. Your tunnel provider, gateway, or
reverse proxy terminates HTTPS and forwards to `127.0.0.1:8788` over the local
machine. The public browser connection is still encrypted end to end up to that
trusted edge. Compose keeps both application ports bound to loopback, so the
DevHub port must never be published directly through a router.

For local use after `docker compose up -d --build`, open
<http://localhost:8787>. Compose explicitly permits HTTP only for these two
host-loopback port mappings; do not change either `127.0.0.1:` port binding to
`0.0.0.0`. If the container was already running, rebuild it for this local
access exception to take effect.

### Mounting below a gateway path

The frontend base path is a build-time setting and defaults to `/`. To publish
the app below `/vault/`, add this to `.env` before building:

```dotenv
DEVHUB_BASE_PATH=/vault/
```

Then rebuild with `docker compose up -d --build`. The browser will request
`/vault/assets/...`, `/vault/api/...`, and `/vault/playcaptcha/...`; configure
your gateway to remove `/vault` before proxying to the DevHub container. To
return to root-path access, set `DEVHUB_BASE_PATH=/` or remove the variable and
rebuild. The backend does not need to know the public prefix.

For public access, keep this setting enabled (it is the default):

```dotenv
DEVHUB_REQUIRE_HTTPS=1
```

DevHub then rejects the public Vault listener unless the trusted gateway marks
the original browser request as HTTPS. Plain HTTP between the gateway and
`127.0.0.1:8788` is acceptable because it never leaves the host. The public URL
must start with `https://`.

Prefer a dedicated hostname such as `vault.example.com` instead of placing the
password manager beside unrelated applications on the same web origin. This
keeps its session cookie and browser trust boundary isolated. A `/vault/`
prefix remains supported when a separate hostname is not available.

For example, Caddy can terminate TLS and publish the Vault endpoint:

```caddy
vault.example.com {
    reverse_proxy 127.0.0.1:8788
}
```

Caddy supplies `X-Forwarded-Proto: https` automatically. With an existing
gateway that strips the optional `/vault/` prefix, preserve the public host and
set the original scheme explicitly. An Nginx location looks like this:

```nginx
location /vault/ {
    proxy_pass http://127.0.0.1:8788/;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-For $remote_addr;
}
```

If your inner-network tunnel already gives you an HTTPS public URL, point it at
`http://127.0.0.1:8788` and ensure it overwrites `X-Forwarded-Proto` with
`https`. If TLS ends at the tunnel provider and a local Nginx receives only the
provider's HTTP hop, use `proxy_set_header X-Forwarded-Proto https;` there and
make sure that Nginx listener is reachable only from the trusted tunnel. Never
expose an `http://` public URL: account passwords, the vault master password,
session cookies, and decrypted entries would otherwise be readable or modifiable
in transit.

Port `8787` remains the host-only maintenance interface and can be opened at
`http://localhost:8787`. Port `8788` is Vault-only and intended for the HTTPS
gateway or tunnel. Never configure a tunnel to target `8787`; the application
also rejects non-loopback Host headers on that listener as a fail-safe.
