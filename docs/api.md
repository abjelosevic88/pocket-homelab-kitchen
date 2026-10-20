# API

Base path `/api/v1`. JSON in and out. Authenticate with the session cookie (browser), `Authorization: Bearer <token>` or `X-API-Key: <token>` (tokens under Settings → API & apps). Money is in **minor units** (`1250` = 12.50) with a `currency` code. Quantities are floats with a free-text `unit` (`g`, `kg`, `ml`, `l`, `tsp`, `tbsp`, `cup`, `pcs`… are understood and merged on lists).

Everything is per person: every recipe, plan entry, list, list line, pantry row, cook and checkout belongs to the account that created it, and every endpoint only sees the caller's own rows (someone else's id answers `404`). API tokens and Pocket Core tokens act as the account they belong to.

### Pocket Core app links

Another Pocket app may call Kitchen for a person with a `pcx_` token Pocket Core issued to it (see [links.md](https://github.com/abjelosevic88/pocket-homelab-core/blob/main/docs/links.md)). Kitchen offers one scope:

| Scope | Name | Routes |
|---|---|---|
| (any link) | | GET `/auth/me` |
| `cooklog:read` | See what you cooked | GET `/cooklog` |

Every other route answers `403` to a link token, and it only ever sees the person's own cooks. Pocket Day uses it for the journal.

Kitchen itself acts in Pocket Inventory (`items:read`, `items:write`, `items:adjust`), Pocket Split (`groups:read`, `expenses:write`) and Pocket Habits (`habits:read`, `entries:write`) when the person chose **Pocket Core** for that app, asking Core for a fresh token with just the scopes each action needs.

## Auth & account
| Method | Path | Notes |
|---|---|---|
| GET | `/auth/status` | `needs_setup`, `auth_mode`, `authenticated`, `user`, `version` |
| POST | `/auth/setup` · `/auth/login` · `/auth/logout` | `{email, password, name}` |
| GET/PUT | `/auth/me` | profile, password change |
| GET/POST/DELETE | `/tokens`, `/tokens/{id}` | personal access tokens |
| GET/POST/PUT/DELETE | `/users`, `/users/{id}` | admin (a member's GET lists only themselves); deleting a user deletes their kitchen |
| GET/PUT | `/settings` · GET `/settings/system` | your own settings and integrations (secrets masked; a masked value means "keep"); `title` / `food_facts` are box-wide and only taken from an admin. Per Pocket app (`inventory`, `split`, `habits`): `*_mode` `core\|direct\|off` (PUT to choose; the default is `direct` with a saved token, else `core` on a box with Pocket Core), `*_via` `core\|direct\|""` (how it works right now, `""` = not connected), `*_detail` (why not, or which link scopes are off); `core_configured`. Clearing `*_url` + `*_token` is Disconnect |
| GET | `/meta` · `/currencies` | lists the UI uses |
| GET | `/dashboard` | for a Homepage widget: `today`, `tomorrow`, `shopping_open`, `pantry_low`, `pantry_expiring`, `cooked_month`, `spend_month_text`… |
| GET | `/home` | everything the home page shows |
| GET | `/search?q=` | recipes + pantry |
| GET | `/backup` · `/export.json` · POST `/import` | SQLite copy of the whole box (admin only); JSON export / merge-import of your own kitchen |

## Recipes
| Method | Path | Notes |
|---|---|---|
| GET | `/recipes?q=&course=&tag=&cuisine=&favorite=1&sort=title\|cooked\|popular\|rating\|time\|new&limit=` | |
| GET | `/recipes/facets` | tags, cuisines, course counts |
| POST | `/recipes` | `title`, `servings`, `prep_minutes`, `cook_minutes`, `course`, `cuisine`, `tags`, `ingredients:[{name, qty, unit, note, optional, group}]`, `steps:[…]`, `nutrition:{}`, `source_url`, `image_url`, `rating`, `notes` |
| POST | `/recipes/import` | `{url, save?}` — schema.org Recipe JSON-LD from any recipe site; without `save` the parsed recipe is returned for editing |
| GET/PUT/DELETE | `/recipes/{id}` | GET returns `{recipe, cooks, planned, pantry}` where `pantry[i]` says what the shelf holds for ingredient *i* |
| POST | `/recipes/{id}/favorite` · `/recipes/{id}/duplicate` | |
| POST | `/recipes/{id}/cook` | `{date?, servings?, notes?, rating?, deduct_pantry?=true, mark_habit?=true, plan_id?}` → `{cook, recipe, deducted, not_in_pantry, habit_marked, warnings}` |
| POST | `/recipes/{id}/shop` | `{list_id?, servings?, skip_pantry?}` → add the ingredients to a list |
| POST/GET/DELETE | `/recipes/{id}/photo` | multipart `file` |
| GET | `/recipes/{id}/export.md` | Markdown |

## Plan
| Method | Path | Notes |
|---|---|---|
| GET | `/plan?from=&to=` | defaults to this week |
| POST | `/plan` | `{date, slot, recipe_id \| title, servings?, notes?}` |
| PUT/DELETE | `/plan/{id}` | PUT also takes `cooked:false` to undo |
| POST | `/plan/{id}/cook` | same body as a recipe cook |
| POST | `/plan/shop` | `{from?, to?, list_id?, skip_pantry?}` → shopping lines for every uncooked recipe |
| POST | `/plan/copy` | `{from, to, days}` — copy a range forward |

## Shopping lists
| Method | Path | Notes |
|---|---|---|
| GET/POST | `/lists` | |
| GET/PUT/DELETE | `/lists/{id}` | GET returns `{list, items}` |
| POST | `/lists/{id}/items` | `{name}` alone is parsed (“2 kg potatoes”, “milk 1.5 l”); or `{name, qty, unit, aisle, note, price, currency}` |
| PUT/DELETE | `/lists/{id}/items/{iid}` · POST `…/toggle` | |
| POST | `/lists/{id}/check` | `{checked}` all lines |
| POST | `/lists/{id}/clear` | drop checked lines (no checkout) |
| POST | `/lists/{id}/low` | add every low pantry item |
| POST | `/lists/{id}/checkout` | `{amount?, currency?, date?, store?, notes?, restock?=true, split?, paid_by?, category?, remove?=true}` → pantry restock, Pocket Split expense, purchase record |
| GET | `/lists/suggest?q=` | item names used before |
| GET | `/purchases` · DELETE `/purchases/{id}` | checkouts (deleting one leaves the pantry and Split untouched) |

## Pantry
| Method | Path | Notes |
|---|---|---|
| GET | `/pantry?q=&location=&category=&low=1&expiring=1` | |
| GET | `/pantry/facets` | locations, categories, counts, last sync |
| POST | `/pantry` | `{name, quantity, unit, min_quantity, location, category, barcode, brand, expiry_date, notes, local?}` — creates the Inventory item too unless `local` |
| GET/PUT/DELETE | `/pantry/{id}` | DELETE `?inventory=archive` also archives the Inventory item |
| POST | `/pantry/{id}/adjust` | `{delta}` or `{quantity}` (+ `note`), through Inventory when mirrored |
| POST | `/pantry/sync` | pull the tagged Inventory items |
| GET | `/lookup?barcode=` | `{source: pantry\|inventory\|openfoodfacts\|none, pantry?, inventory?, product?}` |

## Cook log & integrations
| Method | Path | Notes |
|---|---|---|
| GET | `/cooklog?recipe=&limit=` · DELETE `/cooklog/{id}` | |
| POST | `/integrations/test` | `{which: inventory\|split\|habits\|notify\|foodfacts, url?, key?}`; a typed token is tried directly, otherwise the saved mode decides (Pocket Core or direct) |
| GET | `/split/groups` · `/split/categories` · `/habits/list` · `/inventory/locations` | pass-through lists for the settings page, through whichever connection is in effect (empty without one) |
| POST | `/notify/test` · `/notify/run` | send a test / today's message now |

## Homepage widget

```yaml
- Pocket Kitchen:
    href: https://kitchen.example.com/
    icon: mdi-chef-hat
    widget:
      type: customapi
      url: http://pocket-kitchen:8080/api/v1/dashboard
      headers: { Authorization: "Bearer {{HOMEPAGE_VAR_POCKET_KITCHEN_TOKEN}}" }
      mappings:
        - { field: today, label: Today }
        - { field: shopping_open, label: To buy, format: number }
        - { field: pantry_low, label: Low, format: number }
        - { field: cooked_month, label: Cooked, format: number }
```
