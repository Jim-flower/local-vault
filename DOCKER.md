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
- On another device, open `http://MAC_LAN_IP:8788` for Vault only.

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

## Network security

Port 8788 uses plain HTTP unless you put DevHub behind an HTTPS reverse proxy.
Vault passwords and decrypted entries must not be sent across an untrusted or
public network. Use it only on a trusted private LAN until HTTPS is configured.
