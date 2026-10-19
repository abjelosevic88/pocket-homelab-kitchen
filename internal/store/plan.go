package store

import (
	"context"
	"strings"
)

const planCols = `p.id, p.date, p.slot, p.recipe_id, p.title, p.servings, p.notes, p.cooked_at, p.cook_id, p.sort_order, p.created_by, p.created_at,
	COALESCE(r.title, ''), COALESCE(r.course, ''), COALESCE(r.prep_minutes + r.cook_minutes, 0), COALESCE(r.photo_path, ''), COALESCE(r.image_url, ''), COALESCE(r.servings, 0)`

func scanPlan(row interface{ Scan(...any) error }) (*PlanEntry, error) {
	var p PlanEntry
	var photo string
	if err := row.Scan(&p.ID, &p.Date, &p.Slot, &p.RecipeID, &p.Title, &p.Servings, &p.Notes, &p.CookedAt, &p.CookID, &p.SortOrder, &p.CreatedBy, &p.CreatedAt,
		&p.RecipeTitle, &p.RecipeCourse, &p.RecipeMinutes, &photo, &p.RecipeImageURL, &p.RecipeServings); err != nil {
		return nil, notFound(err)
	}
	p.RecipeHasPhoto = photo != ""
	return &p, nil
}

// ListPlan returns entries between two dates inclusive.
func (s *Store) ListPlan(ctx context.Context, uid int64, from, to string) ([]PlanEntry, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+planCols+` FROM plan p LEFT JOIN recipes r ON r.id = p.recipe_id AND r.user_id = p.user_id WHERE p.user_id = ? AND p.date >= ? AND p.date <= ? ORDER BY p.date, CASE p.slot WHEN 'breakfast' THEN 0 WHEN 'lunch' THEN 1 WHEN 'dinner' THEN 2 ELSE 3 END, p.sort_order, p.id`, uid, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlanEntry{}
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// GetPlan returns one entry.
func (s *Store) GetPlan(ctx context.Context, uid, id int64) (*PlanEntry, error) {
	return scanPlan(s.DB.QueryRowContext(ctx, `SELECT `+planCols+` FROM plan p LEFT JOIN recipes r ON r.id = p.recipe_id AND r.user_id = p.user_id WHERE p.id = ? AND p.user_id = ?`, id, uid))
}

// CreatePlan inserts an entry.
func (s *Store) CreatePlan(ctx context.Context, uid int64, p *PlanEntry) error {
	if uid <= 0 {
		return ErrNotFound
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO plan (date, slot, recipe_id, title, servings, notes, sort_order, created_by, user_id) VALUES (?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(sort_order), 0) + 1 FROM plan WHERE user_id = ? AND date = ? AND slot = ?), ?, ?)`,
		p.Date, p.Slot, nullI(p.RecipeID), strings.TrimSpace(p.Title), p.Servings, p.Notes, uid, p.Date, p.Slot, nullI(p.CreatedBy), uid)
	if err != nil {
		return err
	}
	p.ID, _ = res.LastInsertId()
	np, err := s.GetPlan(ctx, uid, p.ID)
	if err == nil {
		*p = *np
	}
	return nil
}

// UpdatePlan saves date/slot/recipe/title/servings/notes/cooked.
func (s *Store) UpdatePlan(ctx context.Context, uid int64, p *PlanEntry) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE plan SET date = ?, slot = ?, recipe_id = ?, title = ?, servings = ?, notes = ?, cooked_at = ?, cook_id = ?, sort_order = ? WHERE id = ? AND user_id = ?`,
		p.Date, p.Slot, nullI(p.RecipeID), strings.TrimSpace(p.Title), p.Servings, p.Notes, p.CookedAt, nullI(p.CookID), p.SortOrder, p.ID, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	np, err := s.GetPlan(ctx, uid, p.ID)
	if err == nil {
		*p = *np
	}
	return nil
}

// DeletePlan removes an entry.
func (s *Store) DeletePlan(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM plan WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// CountPlannedForRecipe returns how many future (or today) entries use the recipe.
func (s *Store) CountPlannedForRecipe(ctx context.Context, uid, recipeID int64) int {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM plan WHERE user_id = ? AND recipe_id = ? AND date >= ? AND cooked_at = ''`, uid, recipeID, Today()).Scan(&n)
	return n
}

// PlanDatesForRecipe lists upcoming dates the recipe is planned on.
func (s *Store) PlanDatesForRecipe(ctx context.Context, uid, recipeID int64) []string {
	rows, err := s.DB.QueryContext(ctx, `SELECT date FROM plan WHERE user_id = ? AND recipe_id = ? AND date >= ? ORDER BY date LIMIT 5`, uid, recipeID, Today())
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if rows.Scan(&d) == nil {
			out = append(out, d)
		}
	}
	return out
}
