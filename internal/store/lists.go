package store

import (
	"context"
	"strings"
)

// ListLists returns shopping lists with open/checked counts.
func (s *Store) ListLists(ctx context.Context, archived bool) ([]List, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT l.id, l.name, l.store, l.sort_order, l.archived, l.created_at,
		(SELECT COUNT(1) FROM list_items i WHERE i.list_id = l.id AND i.checked = 0), (SELECT COUNT(1) FROM list_items i WHERE i.list_id = l.id AND i.checked = 1)
		FROM lists l WHERE l.archived = ? ORDER BY l.sort_order, l.id`, boolInt(archived))
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

// GetList returns one list.
func (s *Store) GetList(ctx context.Context, id int64) (*List, error) {
	var l List
	var arch int
	err := s.DB.QueryRowContext(ctx, `SELECT l.id, l.name, l.store, l.sort_order, l.archived, l.created_at,
		(SELECT COUNT(1) FROM list_items i WHERE i.list_id = l.id AND i.checked = 0), (SELECT COUNT(1) FROM list_items i WHERE i.list_id = l.id AND i.checked = 1)
		FROM lists l WHERE l.id = ?`, id).Scan(&l.ID, &l.Name, &l.Store, &l.SortOrder, &arch, &l.CreatedAt, &l.Open, &l.Checked)
	if err != nil {
		return nil, notFound(err)
	}
	l.Archived = arch == 1
	return &l, nil
}

// DefaultList returns the first open list, creating "Groceries" when none exists.
func (s *Store) DefaultList(ctx context.Context) (*List, error) {
	ls, err := s.ListLists(ctx, false)
	if err != nil {
		return nil, err
	}
	if len(ls) > 0 {
		return &ls[0], nil
	}
	l := &List{Name: "Groceries"}
	if err := s.CreateList(ctx, l); err != nil {
		return nil, err
	}
	return l, nil
}

// CreateList inserts a list.
func (s *Store) CreateList(ctx context.Context, l *List) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO lists (name, store, sort_order) VALUES (?, ?, (SELECT COALESCE(MAX(sort_order), 0) + 1 FROM lists))`, strings.TrimSpace(l.Name), strings.TrimSpace(l.Store))
	if err != nil {
		return err
	}
	l.ID, _ = res.LastInsertId()
	nl, err := s.GetList(ctx, l.ID)
	if err == nil {
		*l = *nl
	}
	return nil
}

// UpdateList saves name/store/archived.
func (s *Store) UpdateList(ctx context.Context, l *List) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE lists SET name = ?, store = ?, archived = ?, sort_order = ? WHERE id = ?`, strings.TrimSpace(l.Name), strings.TrimSpace(l.Store), boolInt(l.Archived), l.SortOrder, l.ID)
	return err
}

// DeleteList removes a list and its items.
func (s *Store) DeleteList(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM lists WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

const itemCols = `i.id, i.list_id, i.name, i.qty, i.unit, i.note, i.aisle, i.checked, i.recipe_id, i.plan_id, i.pantry_id, i.price, i.currency, i.sort_order, i.created_by, i.created_at, i.updated_at, COALESCE(r.title, '')`

func scanItem(row interface{ Scan(...any) error }) (*ListItem, error) {
	var it ListItem
	var chk int
	if err := row.Scan(&it.ID, &it.ListID, &it.Name, &it.Qty, &it.Unit, &it.Note, &it.Aisle, &chk, &it.RecipeID, &it.PlanID, &it.PantryID, &it.Price, &it.Currency, &it.SortOrder, &it.CreatedBy, &it.CreatedAt, &it.UpdatedAt, &it.RecipeTitle); err != nil {
		return nil, notFound(err)
	}
	it.Checked = chk == 1
	return &it, nil
}

// ListItems returns the lines of a list (open first, by aisle, then checked).
func (s *Store) ListItems(ctx context.Context, listID int64) ([]ListItem, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+itemCols+` FROM list_items i LEFT JOIN recipes r ON r.id = i.recipe_id WHERE i.list_id = ? ORDER BY i.checked, i.sort_order, i.id`, listID)
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

// GetItem returns one line.
func (s *Store) GetItem(ctx context.Context, id int64) (*ListItem, error) {
	return scanItem(s.DB.QueryRowContext(ctx, `SELECT `+itemCols+` FROM list_items i LEFT JOIN recipes r ON r.id = i.recipe_id WHERE i.id = ?`, id))
}

// CreateItem inserts a line.
func (s *Store) CreateItem(ctx context.Context, it *ListItem) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO list_items (list_id, name, qty, unit, note, aisle, checked, recipe_id, plan_id, pantry_id, price, currency, sort_order, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(sort_order), 0) + 1 FROM list_items WHERE list_id = ?), ?)`,
		it.ListID, strings.TrimSpace(it.Name), it.Qty, it.Unit, it.Note, it.Aisle, boolInt(it.Checked), nullI(it.RecipeID), nullI(it.PlanID), nullI(it.PantryID), it.Price, it.Currency, it.ListID, nullI(it.CreatedBy))
	if err != nil {
		return err
	}
	it.ID, _ = res.LastInsertId()
	ni, err := s.GetItem(ctx, it.ID)
	if err == nil {
		*it = *ni
	}
	return nil
}

// UpdateItem saves a line.
func (s *Store) UpdateItem(ctx context.Context, it *ListItem) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE list_items SET list_id = ?, name = ?, qty = ?, unit = ?, note = ?, aisle = ?, checked = ?, recipe_id = ?, plan_id = ?, pantry_id = ?, price = ?, currency = ?, sort_order = ?, updated_at = ? WHERE id = ?`,
		it.ListID, strings.TrimSpace(it.Name), it.Qty, it.Unit, it.Note, it.Aisle, boolInt(it.Checked), nullI(it.RecipeID), nullI(it.PlanID), nullI(it.PantryID), it.Price, it.Currency, it.SortOrder, Now(), it.ID)
	if err != nil {
		return err
	}
	ni, err := s.GetItem(ctx, it.ID)
	if err == nil {
		*it = *ni
	}
	return nil
}

// DeleteItem removes a line.
func (s *Store) DeleteItem(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM list_items WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteCheckedItems clears bought lines; returns how many went.
func (s *Store) DeleteCheckedItems(ctx context.Context, listID int64) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM list_items WHERE list_id = ? AND checked = 1`, listID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SetAllChecked checks or unchecks every line.
func (s *Store) SetAllChecked(ctx context.Context, listID int64, checked bool) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE list_items SET checked = ?, updated_at = ? WHERE list_id = ?`, boolInt(checked), Now(), listID)
	return err
}

// FindOpenItem finds an unchecked line with the same name and unit family for merging.
func (s *Store) FindOpenItem(ctx context.Context, listID int64, name, unit string) (*ListItem, error) {
	return scanItem(s.DB.QueryRowContext(ctx, `SELECT `+itemCols+` FROM list_items i LEFT JOIN recipes r ON r.id = i.recipe_id WHERE i.list_id = ? AND i.checked = 0 AND LOWER(i.name) = ? AND i.unit = ? ORDER BY i.id LIMIT 1`, listID, strings.ToLower(strings.TrimSpace(name)), unit))
}

// OpenItemCount counts unchecked lines across all open lists.
func (s *Store) OpenItemCount(ctx context.Context) int {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM list_items i JOIN lists l ON l.id = i.list_id WHERE l.archived = 0 AND i.checked = 0`).Scan(&n)
	return n
}

// ItemNameSuggestions returns names used before (lists + pantry + ingredients) for autocomplete.
func (s *Store) ItemNameSuggestions(ctx context.Context, limit int) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT name FROM (SELECT name, MAX(created_at) AS t FROM list_items GROUP BY LOWER(name) UNION ALL SELECT name, updated_at FROM pantry WHERE archived = 0) ORDER BY t DESC LIMIT ?`, limit)
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
