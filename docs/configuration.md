# Configuration

Everything is read from environment variables; every value has a default so a bare `docker run -v ./data:/data` works. Integration values seed the settings on first start and can be changed later under **Settings → Integrations** (the UI values win afterwards).

| Variable | Default | What it does |
|---|---|---|
| `PORT` / `HOST` | `8080` / `0.0.0.0` | Listen address |
| `BASE_URL` | – | Public URL, used in notification links |
| `DATA_DIR` | `./data` (`/data` in the image) | SQLite database, recipe photos and backups |
| `DB_PATH` / `FILES_DIR` | `$DATA_DIR/pocket-kitchen.db` / `$DATA_DIR/files` | Override locations |
| `TZ` | – | Container timezone (mount `/usr/share/zoneinfo` on Alpine-based hosts if dates look off) |
| `LOG_LEVEL` / `LOG_FORMAT` | `info` / `text` | `debug`, `warn`, `error`; `json` |
| `TRUST_PROXY` | `true` | Honour `X-Forwarded-For` |
| `METRICS_ENABLED` | `true` | Prometheus `/metrics` |
| `MAX_UPLOAD_BYTES` | `20971520` | Recipe photo upload limit |
| `AUTH_MODE` | `local` | `local` (built-in accounts), `proxy` (trust `AUTH_PROXY_HEADER`), `none` (single user, LAN only) |
| `AUTH_PROXY_HEADER` / `AUTH_PROXY_EMAIL_HEADER` | `Remote-User` / `Remote-Email` | Headers for proxy mode |
| `SESSION_TTL` | `720h` | Cookie lifetime |
| `SECURE_COOKIES` | `false` | Set `true` behind HTTPS |
| `OIDC_ISSUER` / `OIDC_CLIENT_ID` / `OIDC_CLIENT_SECRET` | – | Sign in with an OpenID Connect provider (PocketID, Authelia, Authentik, Keycloak…). Needs `BASE_URL`; the callback is `BASE_URL/api/v1/auth/oidc/callback`. On a box with Pocket Core none of these are needed — the app receives its client when it registers. |
| `OIDC_NAME` | `single sign-on` | Button label: “Sign in with …” (“Pocket Core” when the client came from Core) |
| `OIDC_AUTO_CREATE` | `true` | Give an unknown (verified) email a member account instead of refusing it. Always on for the Core-issued client |
| `ADMIN_EMAIL` / `ADMIN_PASSWORD` / `ADMIN_NAME` | – | Create the first admin on an empty database (otherwise the UI asks) |
| `DEFAULT_CURRENCY` | `EUR` | Home currency for grocery spend |
| `INVENTORY_URL` / `INVENTORY_TOKEN` | – | Pocket Inventory base URL and an API token. These and the `SPLIT_*` / `HABITS_*` / `NOTIFY_*` values below are the owner's (first admin's) own direct connections, each copied once (clearing one in Settings keeps it cleared); other accounts set their own under Settings → Integrations, or use Pocket Core app links with nothing to paste |
| `INVENTORY_TAG` | `pantry` | Inventory items carrying this tag are the pantry |
| `SPLIT_URL` / `SPLIT_TOKEN` / `SPLIT_PUBLIC_URL` / `SPLIT_GROUP_ID` | – | Pocket Split (public URL = what the browser opens for the expense link) |
| `HABITS_URL` / `HABITS_TOKEN` / `HABITS_HABIT_ID` | – | Pocket Habits and the habit to mark when something is cooked |
| `NOTIFY_URL` / `NOTIFY_FORMAT` / `NOTIFY_HOUR` | – / guessed / `9` | Apprise `…/notify/<key>`, ntfy topic URL or JSON webhook; `apprise`, `ntfy`, `json`; hour of the daily message |
| `OPENFOODFACTS` | `true` | Look up unknown barcodes on Open Food Facts |
| `USER_AGENT` | `PocketKitchen/1.0 (…)` | Sent to recipe sites and Open Food Facts |
| `CORE_URL` / `CORE_REGISTRATION_KEY` | – | [Pocket Core](https://git.bjelke.org/abjelosevic88/pocket-homelab-core): register on start (the key is needed once; the minted app token lives in `$DATA_DIR/core.token`), publish `kitchen.cooked` / `kitchen.checkout`, accept the tokens Core hands to paired phones, resolve Inventory / Split / Habits through Core when their `*_URL` is empty, act in every person's own Inventory / Split / Habits through app links (Settings → Integrations → Pocket Core), and offer `cooklog:read` to Pocket Day |
| `CORE_APP_URL` | `BASE_URL` | How Core reaches this app for health checks (e.g. `http://host.docker.internal:8176`) |
| `DEMO_DATA` | `false` | Seed eight recipes, a pantry, a week's plan and a list on an empty database |

## Auth modes

- **local** — accounts with email + password. The first account created becomes admin (the owner). Accounts created on first contact (Pocket Core / OIDC sign-in, Core device tokens) are always members.
- **proxy** — trust a forward-auth proxy (Authelia, Authentik, Tinyauth…). Users are created on first visit. Keep `/api/*` outside the forward-auth rule if a shortcut or the Homepage widget needs it.
- **none** — no login at all; one implicit admin. Only for a trusted LAN.

## CLI

```
pocket-kitchen                     start the server (default)
pocket-kitchen backup [-o FILE]    write a consistent SQLite backup
pocket-kitchen reset-password [-email USER] [-password NEW]
pocket-kitchen version
```

Inside the container: `docker exec pocket-kitchen pocket-kitchen backup`.

## How the pantry works with Pocket Inventory

- Every Inventory item with the pantry tag (default `pantry`) is mirrored into Pocket Kitchen on start, every 30 minutes and on **Pantry → Sync**. Items that lose the tag or get archived are archived here.
- Changing a quantity in Pocket Kitchen (−/+, cooking, a checkout) calls Inventory's `POST /items/{id}/adjust` with a note (“Cooked Pancakes”, “Bought (milk)”), so the movement log in Inventory stays honest.
- Adding a pantry item here creates the Inventory item with the tag (tick “Create it in Pocket Inventory too”; untick for a kitchen-only row).
- Removing a pantry item here only takes the tag off the Inventory item; `DELETE /pantry/{id}?inventory=archive` archives it there too.
- Without Inventory configured the pantry table stands on its own and nothing else changes.
