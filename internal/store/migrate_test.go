package store

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"testing"
)

// TestPerUserMigration builds a database as 0001 left it (a shared household
// kitchen with a member account), runs the per-user migration and checks that
// every row now belongs to the owner and the settings moved with them.
func TestPerUserMigration(t *testing.T) {
	path := t.TempDir() + "/k.db"
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	schema, _ := migrationFS.ReadFile("migrations/0001_init.sql")
	stmts := []string{string(schema),
		`CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`,
		`INSERT INTO schema_migrations VALUES ('0001_init.sql', 'x')`,
		`INSERT INTO users (id, email, role) VALUES (1, 'owner@x', 'admin'), (2, 'member@x', 'member'), (3, 'auto@x', 'admin')`,
		`INSERT INTO users (id, email, role, password_hash) VALUES (4, 'made@x', 'admin', 'hash')`,
		`INSERT INTO recipes (id, title, created_by) VALUES (1, 'Stew', 2), (2, 'Soup', NULL)`,
		`INSERT INTO plan (date, recipe_id) VALUES ('2026-10-01', 1)`,
		`INSERT INTO lists (id, name) VALUES (1, 'Groceries')`,
		`INSERT INTO list_items (list_id, name) VALUES (1, 'milk'), (1, 'eggs')`,
		`INSERT INTO pantry (inventory_id, name) VALUES (42, 'Flour'), (NULL, 'Salt')`,
		`INSERT INTO cooklog (recipe_id, date, user_id) VALUES (1, '2026-10-01', NULL)`,
		`INSERT INTO purchases (date, user_id) VALUES ('2026-10-01', NULL)`,
		`INSERT INTO settings (key, value) VALUES ('title', 'Our kitchen'), ('habits_token', 'secret'), ('habit_id', '6'), ('split_group_id', '1'), ('week_start', 'sun'), ('food_facts', '0')`,
		`INSERT INTO cache (key, value, fetched_at) VALUES ('notify:last_day', '2026-10-19', 'x'), ('off:123', '{}', 'x')`,
	}
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	db.Close()

	st, err := Open(path, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	for _, tbl := range []string{"recipes", "plan", "lists", "list_items", "pantry", "cooklog", "purchases"} {
		var total, owned int
		_ = st.DB.QueryRow(`SELECT COUNT(1), SUM(CASE WHEN user_id = 1 THEN 1 ELSE 0 END) FROM `+tbl).Scan(&total, &owned)
		if total == 0 || total != owned {
			t.Errorf("%s: %d rows, %d owned by the owner", tbl, total, owned)
		}
	}
	if p, err := st.GetPantryByInventory(ctx, 1, 42); err != nil || p.Name != "Flour" {
		t.Errorf("pantry mirror after rebuild: %v %v", p, err)
	}
	// inventory_id is unique per user now, not globally
	if err := st.CreatePantry(ctx, 2, &PantryItem{Name: "Flour", InventoryID: ptr(42)}); err != nil {
		t.Errorf("second user mirroring the same inventory id: %v", err)
	}
	if err := st.CreatePantry(ctx, 1, &PantryItem{Name: "Flour again", InventoryID: ptr(42)}); err != ErrConflict {
		t.Errorf("duplicate mirror for the same user: %v", err)
	}
	owner, _ := st.GetSettings(ctx, 1)
	if owner.HabitsToken != "secret" || owner.HabitID != 6 || owner.SplitGroupID != 1 || owner.WeekStart != "sun" || owner.Title != "Our kitchen" || owner.FoodFacts {
		t.Errorf("owner settings: %+v", owner)
	}
	member, _ := st.GetSettings(ctx, 2)
	if member.HabitsToken != "" || member.HabitID != 0 || member.SplitGroupID != 0 || member.WeekStart != "mon" || member.Title != "Our kitchen" {
		t.Errorf("member settings: %+v", member)
	}
	var left int
	_ = st.DB.QueryRow(`SELECT COUNT(1) FROM settings WHERE key NOT IN ('title', 'food_facts', 'setup_complete')`).Scan(&left)
	if left != 0 {
		t.Errorf("%d personal keys left in the box-wide settings", left)
	}
	if _, _, ok := st.CacheGet(ctx, "notify:last_day:1"); !ok {
		t.Error("notify bookkeeping not moved to the owner")
	}
	for id, want := range map[int64]string{1: "admin", 2: "member", 3: "member", 4: "admin"} {
		if u, _ := st.GetUser(ctx, id); u == nil || u.Role != want {
			t.Errorf("user %d role after migration: %+v, want %s", id, u, want)
		}
	}
	if st.OwnerID(ctx) != 1 {
		t.Errorf("owner = %d", st.OwnerID(ctx))
	}
}

func ptr(i int64) *int64 { return &i }
