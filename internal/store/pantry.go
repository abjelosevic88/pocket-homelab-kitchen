package store

import (
	"context"
	"strings"
	"time"
)

const pantryCols = `id, inventory_id, name, quantity, unit, min_quantity, location, category, barcode, brand, expiry_date, notes, photo_url, archived, updated_at, synced_at`

func scanPantry(row interface{ Scan(...any) error }) (*PantryItem, error) {
	var p PantryItem
	var arch int
	if err := row.Scan(&p.ID, &p.InventoryID, &p.Name, &p.Quantity, &p.Unit, &p.MinQuantity, &p.Location, &p.Category, &p.Barcode, &p.Brand, &p.ExpiryDate, &p.Notes, &p.PhotoURL, &arch, &p.UpdatedAt, &p.SyncedAt); err != nil {
		return nil, notFound(err)
	}
	p.Archived = arch == 1
	p.Low = p.MinQuantity > 0 && p.Quantity <= p.MinQuantity
	if p.ExpiryDate != "" {
		p.Expiring = p.ExpiryDate <= time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	}
	return &p, nil
}

// PantryFilter narrows the pantry list.
type PantryFilter struct {
	Query    string
	Location string
	Category string
	Low      bool
	Expiring bool
	Archived bool
}

// ListPantry returns pantry rows.
func (s *Store) ListPantry(ctx context.Context, uid int64, f PantryFilter) ([]PantryItem, error) {
	where := []string{"user_id = ?", "archived = ?"}
	args := []any{uid, boolInt(f.Archived)}
	if q := strings.TrimSpace(f.Query); q != "" {
		like := "%" + strings.ToLower(q) + "%"
		where = append(where, `(LOWER(name) LIKE ? OR LOWER(brand) LIKE ? OR LOWER(category) LIKE ? OR barcode = ?)`)
		args = append(args, like, like, like, q)
	}
	if f.Location != "" {
		where = append(where, "location = ?")
		args = append(args, f.Location)
	}
	if f.Category != "" {
		where = append(where, "category = ?")
		args = append(args, f.Category)
	}
	if f.Low {
		where = append(where, "min_quantity > 0 AND quantity <= min_quantity")
	}
	if f.Expiring {
		where = append(where, "expiry_date <> '' AND expiry_date <= ?")
		args = append(args, time.Now().AddDate(0, 0, 7).Format("2006-01-02"))
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+pantryCols+` FROM pantry WHERE `+strings.Join(where, " AND ")+` ORDER BY LOWER(location), LOWER(name)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PantryItem{}
	for rows.Next() {
		p, err := scanPantry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// GetPantry returns one of the user's rows.
func (s *Store) GetPantry(ctx context.Context, uid, id int64) (*PantryItem, error) {
	return scanPantry(s.DB.QueryRowContext(ctx, `SELECT `+pantryCols+` FROM pantry WHERE id = ? AND user_id = ?`, id, uid))
}

// GetPantryByInventory finds the user's mirror row of an inventory item.
func (s *Store) GetPantryByInventory(ctx context.Context, uid, invID int64) (*PantryItem, error) {
	return scanPantry(s.DB.QueryRowContext(ctx, `SELECT `+pantryCols+` FROM pantry WHERE user_id = ? AND inventory_id = ?`, uid, invID))
}

// GetPantryByBarcode finds one of the user's rows by barcode.
func (s *Store) GetPantryByBarcode(ctx context.Context, uid int64, code string) (*PantryItem, error) {
	return scanPantry(s.DB.QueryRowContext(ctx, `SELECT `+pantryCols+` FROM pantry WHERE user_id = ? AND barcode = ? AND barcode <> '' ORDER BY archived LIMIT 1`, uid, strings.TrimSpace(code)))
}

// FindPantryByName matches an ingredient name to a pantry row: the same
// name ignoring case and plural ("eggs" = "egg"), or the same name after a
// leading descriptor ("large eggs", "plain flour", "fresh parsley").
func (s *Store) FindPantryByName(ctx context.Context, uid int64, name string) (*PantryItem, error) {
	n := NormalizeName(name)
	if n == "" {
		return nil, ErrNotFound
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+pantryCols+` FROM pantry WHERE user_id = ? AND archived = 0`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var all []PantryItem
	for rows.Next() {
		p, err := scanPantry(rows)
		if err != nil {
			return nil, err
		}
		all = append(all, *p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	cands := []string{n}
	if stripped := stripDescriptors(n); stripped != n && stripped != "" {
		cands = append(cands, stripped)
	}
	for _, c := range cands {
		for i := range all {
			if NormalizeName(all[i].Name) == c {
				return &all[i], nil
			}
		}
	}
	return nil, ErrNotFound
}

var descriptors = map[string]bool{"large": true, "small": true, "medium": true, "fresh": true, "ripe": true, "plain": true, "whole": true, "chopped": true, "diced": true, "sliced": true, "minced": true, "grated": true, "ground": true, "dried": true, "frozen": true, "raw": true, "cooked": true, "organic": true, "free-range": true, "unsalted": true, "salted": true, "extra": true, "virgin": true, "big": true, "little": true, "hot": true, "cold": true, "warm": true, "boiling": true, "all-purpose": true, "self-raising": true, "self-rising": true, "white": true, "brown": true, "red": true, "yellow": true, "green": true, "boneless": true, "skinless": true, "lean": true, "full-fat": true, "low-fat": true, "semi-skimmed": true, "skimmed": true}

func stripDescriptors(n string) string {
	f := strings.Fields(n)
	for len(f) > 1 && descriptors[f[0]] {
		f = f[1:]
	}
	return strings.Join(f, " ")
}

// NormalizeName lower-cases, trims and singularises the last word ("tomatoes" → "tomato").
func NormalizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Join(strings.Fields(s), " ")
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	last := f[len(f)-1]
	switch {
	case strings.HasSuffix(last, "ies") && len(last) > 4:
		last = last[:len(last)-3] + "y"
	case strings.HasSuffix(last, "oes") && len(last) > 4:
		last = last[:len(last)-2]
	case strings.HasSuffix(last, "ches") || strings.HasSuffix(last, "shes") || strings.HasSuffix(last, "sses") || strings.HasSuffix(last, "xes"):
		last = last[:len(last)-2]
	case strings.HasSuffix(last, "s") && !strings.HasSuffix(last, "ss") && len(last) > 3:
		last = last[:len(last)-1]
	}
	f[len(f)-1] = last
	return strings.Join(f, " ")
}

// CreatePantry inserts a row.
func (s *Store) CreatePantry(ctx context.Context, uid int64, p *PantryItem) error {
	if uid <= 0 {
		return ErrNotFound
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO pantry (inventory_id, name, quantity, unit, min_quantity, location, category, barcode, brand, expiry_date, notes, photo_url, archived, synced_at, user_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullI(p.InventoryID), strings.TrimSpace(p.Name), p.Quantity, p.Unit, p.MinQuantity, p.Location, p.Category, strings.TrimSpace(p.Barcode), p.Brand, p.ExpiryDate, p.Notes, p.PhotoURL, boolInt(p.Archived), p.SyncedAt, uid)
	if err != nil {
		return conflict(err)
	}
	p.ID, _ = res.LastInsertId()
	np, err := s.GetPantry(ctx, uid, p.ID)
	if err == nil {
		*p = *np
	}
	return nil
}

// UpdatePantry saves a row.
func (s *Store) UpdatePantry(ctx context.Context, uid int64, p *PantryItem) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE pantry SET inventory_id = ?, name = ?, quantity = ?, unit = ?, min_quantity = ?, location = ?, category = ?, barcode = ?, brand = ?, expiry_date = ?, notes = ?, photo_url = ?, archived = ?, updated_at = ?, synced_at = ? WHERE id = ? AND user_id = ?`,
		nullI(p.InventoryID), strings.TrimSpace(p.Name), p.Quantity, p.Unit, p.MinQuantity, p.Location, p.Category, strings.TrimSpace(p.Barcode), p.Brand, p.ExpiryDate, p.Notes, p.PhotoURL, boolInt(p.Archived), Now(), p.SyncedAt, p.ID, uid)
	if err != nil {
		return conflict(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	np, err := s.GetPantry(ctx, uid, p.ID)
	if err == nil {
		*p = *np
	}
	return nil
}

// DeletePantry removes a row.
func (s *Store) DeletePantry(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM pantry WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ArchiveMirrorsNotIn archives mirror rows whose inventory item is gone from the tag.
func (s *Store) ArchiveMirrorsNotIn(ctx context.Context, uid int64, keep []int64) (int64, error) {
	if len(keep) == 0 {
		res, err := s.DB.ExecContext(ctx, `UPDATE pantry SET archived = 1, updated_at = ? WHERE user_id = ? AND inventory_id IS NOT NULL AND archived = 0`, Now(), uid)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	}
	ph := strings.Repeat("?,", len(keep))
	args := []any{Now(), uid}
	for _, k := range keep {
		args = append(args, k)
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE pantry SET archived = 1, updated_at = ? WHERE user_id = ? AND inventory_id IS NOT NULL AND archived = 0 AND inventory_id NOT IN (`+ph[:len(ph)-1]+`)`, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PantryFacets returns locations and categories in use.
func (s *Store) PantryFacets(ctx context.Context, uid int64) (locations, categories []string, err error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT location, category FROM pantry WHERE user_id = ? AND archived = 0`, uid)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	ls, cs := map[string]bool{}, map[string]bool{}
	for rows.Next() {
		var l, c string
		if err := rows.Scan(&l, &c); err != nil {
			return nil, nil, err
		}
		if l != "" {
			ls[l] = true
		}
		if c != "" {
			cs[c] = true
		}
	}
	for l := range ls {
		locations = append(locations, l)
	}
	for c := range cs {
		categories = append(categories, c)
	}
	sortStrings(locations)
	sortStrings(categories)
	return locations, categories, rows.Err()
}

// PantryStats counts rows / low / expiring.
func (s *Store) PantryStats(ctx context.Context, uid int64) (total, low, expiring int) {
	var l, e *int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1), SUM(CASE WHEN min_quantity > 0 AND quantity <= min_quantity THEN 1 ELSE 0 END), SUM(CASE WHEN expiry_date <> '' AND expiry_date <= ? THEN 1 ELSE 0 END) FROM pantry WHERE user_id = ? AND archived = 0`, time.Now().AddDate(0, 0, 7).Format("2006-01-02"), uid).Scan(&total, &l, &e)
	if l != nil {
		low = *l
	}
	if e != nil {
		expiring = *e
	}
	return
}
