-- Every row of personal content belongs to exactly one user. Until now the
-- whole kitchen was shared by the household; everything that exists belongs to
-- the owner: the first admin (or, failing that, the first account).
--
-- Child rows: list_items carry their own user_id (kept equal to the list's) so
-- every query can filter on it directly instead of joining through lists.

ALTER TABLE recipes    ADD COLUMN user_id INTEGER REFERENCES users(id) ON DELETE CASCADE;
ALTER TABLE plan       ADD COLUMN user_id INTEGER REFERENCES users(id) ON DELETE CASCADE;
ALTER TABLE lists      ADD COLUMN user_id INTEGER REFERENCES users(id) ON DELETE CASCADE;
ALTER TABLE list_items ADD COLUMN user_id INTEGER REFERENCES users(id) ON DELETE CASCADE;
-- cooklog.user_id and purchases.user_id already exist (who cooked / who paid);
-- they now also mean "whose record this is".

UPDATE recipes SET user_id = COALESCE((SELECT id FROM users WHERE role = 'admin' ORDER BY id LIMIT 1), (SELECT MIN(id) FROM users)) WHERE user_id IS NULL;
UPDATE plan    SET user_id = COALESCE((SELECT id FROM users WHERE role = 'admin' ORDER BY id LIMIT 1), (SELECT MIN(id) FROM users)) WHERE user_id IS NULL;
UPDATE lists   SET user_id = COALESCE((SELECT id FROM users WHERE role = 'admin' ORDER BY id LIMIT 1), (SELECT MIN(id) FROM users)) WHERE user_id IS NULL;
UPDATE list_items SET user_id = (SELECT l.user_id FROM lists l WHERE l.id = list_items.list_id) WHERE user_id IS NULL;
UPDATE cooklog   SET user_id = COALESCE((SELECT id FROM users WHERE role = 'admin' ORDER BY id LIMIT 1), (SELECT MIN(id) FROM users)) WHERE user_id IS NULL;
UPDATE purchases SET user_id = COALESCE((SELECT id FROM users WHERE role = 'admin' ORDER BY id LIMIT 1), (SELECT MIN(id) FROM users)) WHERE user_id IS NULL;

-- The pantry is rebuilt: inventory_id was globally UNIQUE, now it is unique per
-- user (two people may each mirror an item their own Inventory token sees).
-- Nothing references pantry by foreign key, so the swap is safe.
CREATE TABLE pantry_new (
  id           INTEGER PRIMARY KEY,
  user_id      INTEGER REFERENCES users(id) ON DELETE CASCADE,
  inventory_id INTEGER,
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
  synced_at    TEXT NOT NULL DEFAULT '',
  UNIQUE (user_id, inventory_id)
);
INSERT INTO pantry_new (id, user_id, inventory_id, name, quantity, unit, min_quantity, location, category, barcode, brand, expiry_date, notes, photo_url, archived, updated_at, synced_at)
  SELECT id, COALESCE((SELECT id FROM users WHERE role = 'admin' ORDER BY id LIMIT 1), (SELECT MIN(id) FROM users)),
         inventory_id, name, quantity, unit, min_quantity, location, category, barcode, brand, expiry_date, notes, photo_url, archived, updated_at, synced_at
  FROM pantry;
DROP TABLE pantry;
ALTER TABLE pantry_new RENAME TO pantry;
CREATE INDEX pantry_user_name ON pantry(user_id, name);

CREATE INDEX recipes_user    ON recipes(user_id, archived);
CREATE INDEX plan_user_date  ON plan(user_id, date);
CREATE INDEX lists_user      ON lists(user_id, archived);
CREATE INDEX list_items_user ON list_items(user_id, list_id);
CREATE INDEX cooklog_user    ON cooklog(user_id, date);
CREATE INDEX purchases_user  ON purchases(user_id, date);

-- Personal settings (preferences, notifications, the Inventory / Split /
-- Habits connections with their tokens and ids) move to one row set per user.
-- What was configured so far is the owner's. Box-wide settings (title,
-- Open Food Facts, setup_complete) stay in `settings`.
CREATE TABLE user_settings (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key     TEXT NOT NULL,
  value   TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, key)
);
INSERT INTO user_settings (user_id, key, value)
  SELECT COALESCE((SELECT id FROM users WHERE role = 'admin' ORDER BY id LIMIT 1), (SELECT MIN(id) FROM users)), key, value
  FROM settings
  WHERE key IN ('home_currency','date_format','time_format','week_start','default_servings',
                'notify_url','notify_format','notify_hour','notify_plan','notify_expiry',
                'inventory_url','inventory_token','inventory_tag','inventory_location','inventory_auto_sync',
                'split_url','split_token','split_public_url','split_group_id','split_member_id','split_category',
                'habits_url','habits_token','habit_id')
    AND EXISTS (SELECT 1 FROM users);
DELETE FROM settings
  WHERE key IN ('home_currency','date_format','time_format','week_start','default_servings',
                'notify_url','notify_format','notify_hour','notify_plan','notify_expiry',
                'inventory_url','inventory_token','inventory_tag','inventory_location','inventory_auto_sync',
                'split_url','split_token','split_public_url','split_group_id','split_member_id','split_category',
                'habits_url','habits_token','habit_id')
    AND EXISTS (SELECT 1 FROM users);

-- Per-user bookkeeping keys in the cache (last daily push, last pantry sync).
UPDATE cache SET key = key || ':' || COALESCE((SELECT id FROM users WHERE role = 'admin' ORDER BY id LIMIT 1), (SELECT MIN(id) FROM users))
  WHERE key IN ('notify:last_day', 'pantry:synced_at') AND EXISTS (SELECT 1 FROM users);

-- Only the owner may be admin by default. Accounts created on first contact
-- (Core / PocketID sign-in, Core device tokens) never carry a password; an
-- admin like that who is not the owner can only come from auto-creation, so it
-- becomes a member. Admins someone created on purpose (with a password) stay.
UPDATE users SET role = 'member'
  WHERE role = 'admin' AND password_hash = ''
    AND id <> (SELECT id FROM users WHERE role = 'admin' ORDER BY id LIMIT 1);
