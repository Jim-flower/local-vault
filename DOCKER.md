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

DevHub itself does not terminate TLS and accepts HTTP from a gateway or reverse
proxy. The Compose file keeps both application ports bound to loopback, so the
application must not be published directly through a router.

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

Put a gateway or reverse proxy in front of `127.0.0.1:8788`. It may forward
HTTP for a trusted LAN or VPN. For example, this Caddy configuration publishes
only the Vault endpoint over HTTP:

```caddy
http://vault.example.com {
    reverse_proxy 127.0.0.1:8788
}
```

**Do not use this HTTP configuration on the public internet.** Account
passwords, vault-master passwords, and decrypted entries can be read or changed
by anyone able to intercept the connection. Use it only behind a VPN or a
trusted private network. Do not expose the Docker application port itself.
