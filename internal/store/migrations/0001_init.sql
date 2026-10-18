CREATE TABLE users (
  id            INTEGER PRIMARY KEY,
  email         TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'admin',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT
);

CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE api_tokens (
  id           INTEGER PRIMARY KEY,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL DEFAULT '',
  token_hash   TEXT NOT NULL UNIQUE,
  prefix       TEXT NOT NULL DEFAULT '',
  last_used_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);

-- A recipe is the unit everything hangs off: the plan points at it, the
-- shopping list is generated from its ingredients, cooking it deducts them
-- from the pantry. Ingredients and steps are JSON so a recipe is one row.
CREATE TABLE recipes (
  id           INTEGER PRIMARY KEY,
  title        TEXT NOT NULL,
  description  TEXT NOT NULL DEFAULT '',
  servings     INTEGER NOT NULL DEFAULT 2,
  prep_minutes INTEGER NOT NULL DEFAULT 0,
  cook_minutes INTEGER NOT NULL DEFAULT 0,
  course       TEXT NOT NULL DEFAULT 'dinner',     -- breakfast | lunch | dinner | dessert | snack | side | drink | other
  cuisine      TEXT NOT NULL DEFAULT '',
  tags         TEXT NOT NULL DEFAULT '',           -- comma separated
  source_url   TEXT NOT NULL DEFAULT '',
  source_name  TEXT NOT NULL DEFAULT '',
  rating       INTEGER NOT NULL DEFAULT 0,         -- 0..5
  favorite     INTEGER NOT NULL DEFAULT 0,
  notes        TEXT NOT NULL DEFAULT '',
  photo_path   TEXT NOT NULL DEFAULT '',
  image_url    TEXT NOT NULL DEFAULT '',           -- remote image when nothing was uploaded
  ingredients  TEXT NOT NULL DEFAULT '[]',         -- JSON [{name, qty, unit, note, pantry_id, optional, group}]
  steps        TEXT NOT NULL DEFAULT '[]',         -- JSON [string]
  nutrition    TEXT NOT NULL DEFAULT '{}',         -- JSON {calories, protein, fat, carbs…} per serving, free form
  times_cooked INTEGER NOT NULL DEFAULT 0,
  last_cooked  TEXT NOT NULL DEFAULT '',
  archived     INTEGER NOT NULL DEFAULT 0,
  created_by   INTEGER,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX recipes_title ON recipes(title);

-- The meal plan: one row per slot per day. Either a recipe or free text
-- ("leftovers", "eating out"). Cooking it writes a cook log row.
CREATE TABLE plan (
  id         INTEGER PRIMARY KEY,
  date       TEXT NOT NULL,                        -- YYYY-MM-DD
  slot       TEXT NOT NULL DEFAULT 'dinner',       -- breakfast | lunch | dinner | snack
  recipe_id  INTEGER REFERENCES recipes(id) ON DELETE SET NULL,
  title      TEXT NOT NULL DEFAULT '',             -- free text when recipe_id is NULL
  servings   INTEGER NOT NULL DEFAULT 0,           -- 0 = recipe default
  notes      TEXT NOT NULL DEFAULT '',
  cooked_at  TEXT NOT NULL DEFAULT '',
  cook_id    INTEGER,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_by INTEGER,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX plan_date ON plan(date, slot, sort_order);

CREATE TABLE lists (
  id         INTEGER PRIMARY KEY,
  name       TEXT NOT NULL,
  store      TEXT NOT NULL DEFAULT '',
  sort_order INTEGER NOT NULL DEFAULT 0,
  archived   INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Shopping list lines. Generated lines remember the recipe / plan entry they
-- came from; a checked line with a price feeds the checkout total.
CREATE TABLE list_items (
  id         INTEGER PRIMARY KEY,
  list_id    INTEGER NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  qty        REAL NOT NULL DEFAULT 0,
  unit       TEXT NOT NULL DEFAULT '',
  note       TEXT NOT NULL DEFAULT '',
  aisle      TEXT NOT NULL DEFAULT '',             -- produce | dairy | meat | bakery | pantry | frozen | drinks | household | other
  checked    INTEGER NOT NULL DEFAULT 0,
  recipe_id  INTEGER REFERENCES recipes(id) ON DELETE SET NULL,
  plan_id    INTEGER REFERENCES plan(id) ON DELETE SET NULL,
  pantry_id  INTEGER,
  price      INTEGER NOT NULL DEFAULT 0,           -- minor units, optional
  currency   TEXT NOT NULL DEFAULT '',
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_by INTEGER,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX list_items_list ON list_items(list_id, checked, sort_order);

-- The pantry. With Pocket Inventory connected every row mirrors an inventory
-- item carrying the pantry tag (inventory_id set) and quantity changes go
-- through Inventory's adjust endpoint; without it the table stands alone.
CREATE TABLE pantry (
  id           INTEGER PRIMARY KEY,
  inventory_id INTEGER UNIQUE,
  name         TEXT NOT NULL,
  quantity     REAL NOT NULL DEFAULT 0,
  unit         TEXT NOT NULL DEFAULT '',
  min_quantity REAL NOT NULL DEFAULT 0,
  location     TEXT NOT NULL DEFAULT '',
  category     TEXT NOT NULL DEFAULT '',
  barcode      TEXT NOT NULL DEFAULT '',
  brand        TEXT NOT NULL DEFAULT '',
  expiry_date  TEXT NOT NULL DEFAULT '',
  notes        TEXT NOT NULL DEFAULT '',
  photo_url    TEXT NOT NULL DEFAULT '',
  archived     INTEGER NOT NULL DEFAULT 0,
  updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  synced_at    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX pantry_name ON pantry(name);

-- Every time something was cooked.
CREATE TABLE cooklog (
  id              INTEGER PRIMARY KEY,
  recipe_id       INTEGER REFERENCES recipes(id) ON DELETE SET NULL,
  plan_id         INTEGER,
  date            TEXT NOT NULL,
  servings        INTEGER NOT NULL DEFAULT 0,
  notes           TEXT NOT NULL DEFAULT '',
  rating          INTEGER NOT NULL DEFAULT 0,
  user_id         INTEGER,
  habit_marked    INTEGER NOT NULL DEFAULT 0,
  pantry_deducted INTEGER NOT NULL DEFAULT 0,     -- how many ingredient lines came off the pantry
  created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX cooklog_date ON cooklog(date);

-- A checkout: the checked lines of a list were bought. Remembers what went
-- to Pocket Split and whether the pantry was restocked.
CREATE TABLE purchases (
  id               INTEGER PRIMARY KEY,
  list_id          INTEGER,
  list_name        TEXT NOT NULL DEFAULT '',
  date             TEXT NOT NULL,
  store            TEXT NOT NULL DEFAULT '',
  amount           INTEGER NOT NULL DEFAULT 0,
  currency         TEXT NOT NULL DEFAULT '',
  items            INTEGER NOT NULL DEFAULT 0,
  item_names       TEXT NOT NULL DEFAULT '',
  paid_by          TEXT NOT NULL DEFAULT '',
  split_group_id   INTEGER NOT NULL DEFAULT 0,
  split_expense_id INTEGER NOT NULL DEFAULT 0,
  pantry_restocked INTEGER NOT NULL DEFAULT 0,
  notes            TEXT NOT NULL DEFAULT '',
  user_id          INTEGER,
  created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Cached answers from Inventory, Split, Habits, Open Food Facts… keyed by what was asked.
CREATE TABLE cache (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  fetched_at TEXT NOT NULL
);
