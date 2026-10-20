package store

import (
	"context"
	"strings"
)

// ListLists returns the user's shopping lists with open/checked counts.
func (s *Store) ListLists(ctx context.Context, uid int64, archived bool) ([]List, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT l.id, l.name, l.store, l.sort_order, l.archived, l.created_at,
		(SELECT COUNT(1) FROM list_items i WHERE i.list_id = l.id AND i.checked = 0), (SELECT COUNT(1) FROM list_items i WHERE i.list_id = l.id AND i.checked = 1)
		FROM lists l WHERE l.user_id = ? AND l.archived = ? ORDER BY l.sort_order, l.id`, uid, boolInt(archived))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []List{}
	for rows.Next() {
		var l List
		var arch int
		if err := rows.Scan(&l.ID, &l.Name, &l.Store, &l.SortOrder, &arch, &l.CreatedAt, &l.Open, &l.Checked); err != nil {
			return nil, err
		}
		l.Archived = arch == 1
		out = append(out, l)
	}
	return out, rows.Err()
}

// GetList returns one of the user's lists.
func (s *Store) GetList(ctx context.Context, uid, id int64) (*List, error) {
	var l List
	var arch int
	err := s.DB.QueryRowContext(ctx, `SELECT l.id, l.name, l.store, l.sort_order, l.archived, l.created_at,
		(SELECT COUNT(1) FROM list_items i WHERE i.list_id = l.id AND i.checked = 0), (SELECT COUNT(1) FROM list_items i WHERE i.list_id = l.id AND i.checked = 1)
		FROM lists l WHERE l.id = ? AND l.user_id = ?`, id, uid).Scan(&l.ID, &l.Name, &l.Store, &l.SortOrder, &arch, &l.CreatedAt, &l.Open, &l.Checked)
	if err != nil {
		return nil, notFound(err)
	}
	l.Archived = arch == 1
	return &l, nil
}

// DefaultList returns the user's first open list, creating "Groceries" when none exists.
func (s *Store) DefaultList(ctx context.Context, uid int64) (*List, error) {
	ls, err := s.ListLists(ctx, uid, false)
	if err != nil {
		return nil, err
	}
	if len(ls) > 0 {
		return &ls[0], nil
	}
	l := &List{Name: "Groceries"}
	if err := s.CreateList(ctx, uid, l); err != nil {
		return nil, err
	}
	return l, nil
}

// CreateList inserts a list owned by uid.
func (s *Store) CreateList(ctx context.Context, uid int64, l *List) error {
	if uid <= 0 {
		return ErrNotFound
	}
	var err error
	l.ID, err = reserveResourceID(ctx, s.DB, "lists")
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO lists (id, name, store, sort_order, user_id) VALUES (?, ?, ?, (SELECT COALESCE(MAX(sort_order), 0) + 1 FROM lists WHERE user_id = ?), ?)`, l.ID, strings.TrimSpace(l.Name), strings.TrimSpace(l.Store), uid, uid)
	if err != nil {
		return err
	}
	nl, err := s.GetList(ctx, uid, l.ID)
	if err == nil {
		*l = *nl
	}
	return nil
}

// UpdateList saves name/store/archived.
func (s *Store) UpdateList(ctx context.Context, uid int64, l *List) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE lists SET name = ?, store = ?, archived = ?, sort_order = ? WHERE id = ? AND user_id = ?`, strings.TrimSpace(l.Name), strings.TrimSpace(l.Store), boolInt(l.Archived), l.SortOrder, l.ID, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteList removes a list and its items.
func (s *Store) DeleteList(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM lists WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

const itemCols = `i.id, i.list_id, i.name, i.qty, i.unit, i.note, i.aisle, i.checked, i.recipe_id, i.plan_id, i.pantry_id, i.price, i.currency, i.sort_order, i.created_by, i.created_at, i.updated_at, COALESCE(r.title, '')`

const itemFrom = ` FROM list_items i LEFT JOIN recipes r ON r.id = i.recipe_id AND r.user_id = i.user_id`

func scanItem(row interface{ Scan(...any) error }) (*ListItem, error) {
	var it ListItem
	var chk int
	if err := row.Scan(&it.ID, &it.ListID, &it.Name, &it.Qty, &it.Unit, &it.Note, &it.Aisle, &chk, &it.RecipeID, &it.PlanID, &it.PantryID, &it.Price, &it.Currency, &it.SortOrder, &it.CreatedBy, &it.CreatedAt, &it.UpdatedAt, &it.RecipeTitle); err != nil {
		return nil, notFound(err)
	}
	it.Checked = chk == 1
	return &it, nil
}

// ListItems returns the lines of one of the user's lists (open first, by aisle, then checked).
func (s *Store) ListItems(ctx context.Context, uid, listID int64) ([]ListItem, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+itemCols+itemFrom+` WHERE i.user_id = ? AND i.list_id = ? ORDER BY i.checked, i.sort_order, i.id`, uid, listID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ListItem{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// GetItem returns one of the user's lines.
func (s *Store) GetItem(ctx context.Context, uid, id int64) (*ListItem, error) {
	return scanItem(s.DB.QueryRowContext(ctx, `SELECT `+itemCols+itemFrom+` WHERE i.id = ? AND i.user_id = ?`, id, uid))
}

func (s *Store) ownsList(ctx context.Context, uid, listID int64) bool {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM lists WHERE id = ? AND user_id = ?`, listID, uid).Scan(&n)
	return n > 0
}

// CreateItem inserts a line on one of the user's lists.
func (s *Store) CreateItem(ctx context.Context, uid int64, it *ListItem) error {
	if uid <= 0 || !s.ownsList(ctx, uid, it.ListID) {
		return ErrNotFound
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO list_items (list_id, name, qty, unit, note, aisle, checked, recipe_id, plan_id, pantry_id, price, currency, sort_order, created_by, user_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(sort_order), 0) + 1 FROM list_items WHERE list_id = ?), ?, ?)`,
		it.ListID, strings.TrimSpace(it.Name), it.Qty, it.Unit, it.Note, it.Aisle, boolInt(it.Checked), nullI(it.RecipeID), nullI(it.PlanID), nullI(it.PantryID), it.Price, it.Currency, it.ListID, nullI(it.CreatedBy), uid)
	if err != nil {
		return err
	}
	it.ID, _ = res.LastInsertId()
	ni, err := s.GetItem(ctx, uid, it.ID)
	if err == nil {
		*it = *ni
	}
	return nil
}

// UpdateItem saves a line (it may move to another of the user's lists).
func (s *Store) UpdateItem(ctx context.Context, uid int64, it *ListItem) error {
	if !s.ownsList(ctx, uid, it.ListID) {
		return ErrNotFound
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE list_items SET list_id = ?, name = ?, qty = ?, unit = ?, note = ?, aisle = ?, checked = ?, recipe_id = ?, plan_id = ?, pantry_id = ?, price = ?, currency = ?, sort_order = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		it.ListID, strings.TrimSpace(it.Name), it.Qty, it.Unit, it.Note, it.Aisle, boolInt(it.Checked), nullI(it.RecipeID), nullI(it.PlanID), nullI(it.PantryID), it.Price, it.Currency, it.SortOrder, Now(), it.ID, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	ni, err := s.GetItem(ctx, uid, it.ID)
	if err == nil {
		*it = *ni
	}
	return nil
}

// DeleteItem removes a line.
func (s *Store) DeleteItem(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM list_items WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteCheckedItems clears bought lines; returns how many went.
func (s *Store) DeleteCheckedItems(ctx context.Context, uid, listID int64) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM list_items WHERE user_id = ? AND list_id = ? AND checked = 1`, uid, listID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SetAllChecked checks or unchecks every line.
func (s *Store) SetAllChecked(ctx context.Context, uid, listID int64, checked bool) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE list_items SET checked = ?, updated_at = ? WHERE user_id = ? AND list_id = ?`, boolInt(checked), Now(), uid, listID)
	return err
}

// FindOpenItem finds an unchecked line with the same name and unit family for merging.
func (s *Store) FindOpenItem(ctx context.Context, uid, listID int64, name, unit string) (*ListItem, error) {
	return scanItem(s.DB.QueryRowContext(ctx, `SELECT `+itemCols+itemFrom+` WHERE i.user_id = ? AND i.list_id = ? AND i.checked = 0 AND LOWER(i.name) = ? AND i.unit = ? ORDER BY i.id LIMIT 1`, uid, listID, strings.ToLower(strings.TrimSpace(name)), unit))
}

// OpenItemCount counts unchecked lines across the user's open lists.
func (s *Store) OpenItemCount(ctx context.Context, uid int64) int {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM list_items i JOIN lists l ON l.id = i.list_id WHERE i.user_id = ? AND l.archived = 0 AND i.checked = 0`, uid).Scan(&n)
	return n
}

// OpenItemCountAll counts unchecked lines on every open list (box-wide metrics only).
func (s *Store) OpenItemCountAll(ctx context.Context) int {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM list_items i JOIN lists l ON l.id = i.list_id WHERE l.archived = 0 AND i.checked = 0`).Scan(&n)
	return n
}

// ItemNameSuggestions returns names the user used before (lists + pantry) for autocomplete.
func (s *Store) ItemNameSuggestions(ctx context.Context, uid int64, limit int) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT name FROM (SELECT name, MAX(created_at) AS t FROM list_items WHERE user_id = ? GROUP BY LOWER(name) UNION ALL SELECT name, updated_at FROM pantry WHERE user_id = ? AND archived = 0) ORDER BY t DESC LIMIT ?`, uid, uid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	out := []string{}
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			if k := strings.ToLower(n); !seen[k] {
				seen[k] = true
				out = append(out, n)
			}
		}
	}
	return out, rows.Err()
}
