package store

import (
	"context"
	"strconv"
)

// DefaultTitle is the app title until someone changes it.
const DefaultTitle = "Pocket Kitchen"

// boxKeys are the settings shared by the whole box (admin-only); every other
// key is personal and lives in user_settings.
var boxKeys = map[string]bool{"title": true, "food_facts": true, "setup_complete": true}

// defaults returns the settings a person starts with.
func (s *Store) defaults() Settings {
	cur := s.DefaultCurrency
	if cur == "" {
		cur = "EUR"
	}
	return Settings{Title: DefaultTitle, HomeCurrency: cur, DateFormat: "auto", TimeFormat: "auto", WeekStart: "mon", DefaultServings: 2, NotifyFormat: "apprise", NotifyHour: 9, NotifyPlan: true, NotifyExpiry: true,
		InventoryTag: "pantry", InventoryAutoSync: true, SplitCategory: "groceries", FoodFacts: true}
}

// GetSettings loads the box-wide settings plus the personal settings of one
// user (uid 0 = box-wide only, personal fields at their defaults).
func (s *Store) GetSettings(ctx context.Context, uid int64) (Settings, error) {
	st := s.defaults()
	kv := map[string]string{}
	rows, err := s.DB.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			rows.Close()
			return st, err
		}
		if boxKeys[k] {
			kv[k] = v
		}
	}
	rows.Close()
	if uid > 0 {
		rows, err := s.DB.QueryContext(ctx, `SELECT key, value FROM user_settings WHERE user_id = ?`, uid)
		if err != nil {
			return st, err
		}
		for rows.Next() {
			var k, v string
			if err := rows.Scan(&k, &v); err != nil {
				rows.Close()
				return st, err
			}
			if !boxKeys[k] {
				kv[k] = v
			}
		}
		rows.Close()
	}
	for k, v := range kv {
		applySetting(&st, k, v)
	}
	return st, nil
}

func applySetting(st *Settings, k, v string) {
	switch k {
	case "title":
		if v != "" {
			st.Title = v
		}
	case "home_currency":
		if v != "" {
			st.HomeCurrency = v
		}
	case "date_format":
		if v != "" {
			st.DateFormat = v
		}
	case "time_format":
		if v != "" {
			st.TimeFormat = v
		}
	case "week_start":
		if v == "sun" {
			st.WeekStart = "sun"
		}
	case "default_servings":
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			st.DefaultServings = n
		}
	case "notify_url":
		st.NotifyURL = v
	case "notify_format":
		if v != "" {
			st.NotifyFormat = v
		}
	case "notify_hour":
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 23 {
			st.NotifyHour = n
		}
	case "notify_plan":
		st.NotifyPlan = v != "0"
	case "notify_expiry":
		st.NotifyExpiry = v != "0"
	case "inventory_url":
		st.InventoryURL = v
	case "inventory_token":
		st.InventoryToken = v
	case "inventory_tag":
		if v != "" {
			st.InventoryTag = v
		}
	case "inventory_location":
		st.InventoryLocation = v
	case "inventory_auto_sync":
		st.InventoryAutoSync = v != "0"
	case "split_url":
		st.SplitURL = v
	case "split_token":
		st.SplitToken = v
	case "split_public_url":
		st.SplitPublicURL = v
	case "split_group_id":
		st.SplitGroupID, _ = strconv.ParseInt(v, 10, 64)
	case "split_member_id":
		st.SplitMemberID, _ = strconv.ParseInt(v, 10, 64)
	case "split_category":
		if v != "" {
			st.SplitCategory = v
		}
	case "habits_url":
		st.HabitsURL = v
	case "habits_token":
		st.HabitsToken = v
	case "habit_id":
		st.HabitID, _ = strconv.ParseInt(v, 10, 64)
	case "inventory_mode":
		st.InventoryMode = v
	case "split_mode":
		st.SplitMode = v
	case "habits_mode":
		st.HabitsMode = v
	case "food_facts":
		st.FoodFacts = v != "0"
	case "setup_complete":
		st.SetupComplete = v == "1"
	}
}

func settingsKV(st Settings) map[string]string {
	b := map[bool]string{true: "1", false: "0"}
	return map[string]string{
		"title":               st.Title,
		"home_currency":       st.HomeCurrency,
		"date_format":         st.DateFormat,
		"time_format":         st.TimeFormat,
		"week_start":          st.WeekStart,
		"default_servings":    strconv.Itoa(st.DefaultServings),
		"notify_url":          st.NotifyURL,
		"notify_format":       st.NotifyFormat,
		"notify_hour":         strconv.Itoa(st.NotifyHour),
		"notify_plan":         b[st.NotifyPlan],
		"notify_expiry":       b[st.NotifyExpiry],
		"inventory_url":       st.InventoryURL,
		"inventory_token":     st.InventoryToken,
		"inventory_tag":       st.InventoryTag,
		"inventory_location":  st.InventoryLocation,
		"inventory_auto_sync": b[st.InventoryAutoSync],
		"split_url":           st.SplitURL,
		"split_token":         st.SplitToken,
		"split_public_url":    st.SplitPublicURL,
		"split_group_id":      strconv.FormatInt(st.SplitGroupID, 10),
		"split_member_id":     strconv.FormatInt(st.SplitMemberID, 10),
		"split_category":      st.SplitCategory,
		"habits_url":          st.HabitsURL,
		"habits_token":        st.HabitsToken,
		"habit_id":            strconv.FormatInt(st.HabitID, 10),
		"inventory_mode":      st.InventoryMode,
		"split_mode":          st.SplitMode,
		"habits_mode":         st.HabitsMode,
		"food_facts":          b[st.FoodFacts],
		"setup_complete":      b[st.SetupComplete],
	}
}

// SaveBoxSettings persists the box-wide settings (title, Open Food Facts, setup flag).
func (s *Store) SaveBoxSettings(ctx context.Context, st Settings) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for k, v := range settingsKV(st) {
		if !boxKeys[k] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// SaveUserSettings persists one user's personal settings.
func (s *Store) SaveUserSettings(ctx context.Context, uid int64, st Settings) error {
	if uid <= 0 {
		return ErrNotFound
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for k, v := range settingsKV(st) {
		if boxKeys[k] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_settings (user_id, key, value) VALUES (?, ?, ?) ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value`, uid, k, v); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// SettingsUsers lists the users who have any personal settings stored (the
// candidates for background work: pantry sync, the daily push).
func (s *Store) SettingsUsers(ctx context.Context) ([]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT us.user_id FROM user_settings us JOIN users u ON u.id = us.user_id ORDER BY us.user_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
