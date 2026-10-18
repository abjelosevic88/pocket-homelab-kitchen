package store

import "context"

const cookCols = `c.id, c.recipe_id, c.plan_id, c.date, c.servings, c.notes, c.rating, c.user_id, c.habit_marked, c.pantry_deducted, c.created_at, COALESCE(r.title, ''), COALESCE(u.name, u.email, '')`

func scanCook(row interface{ Scan(...any) error }) (*Cook, error) {
	var c Cook
	var hm int
	if err := row.Scan(&c.ID, &c.RecipeID, &c.PlanID, &c.Date, &c.Servings, &c.Notes, &c.Rating, &c.UserID, &hm, &c.PantryDeducted, &c.CreatedAt, &c.RecipeTitle, &c.UserName); err != nil {
		return nil, notFound(err)
	}
	c.HabitMarked = hm == 1
	return &c, nil
}

// CreateCook inserts a cook-log row.
func (s *Store) CreateCook(ctx context.Context, c *Cook) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO cooklog (recipe_id, plan_id, date, servings, notes, rating, user_id, habit_marked, pantry_deducted) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullI(c.RecipeID), nullI(c.PlanID), c.Date, c.Servings, c.Notes, c.Rating, nullI(c.UserID), boolInt(c.HabitMarked), c.PantryDeducted)
	if err != nil {
		return err
	}
	c.ID, _ = res.LastInsertId()
	return nil
}

// UpdateCookFlags records what the integrations did.
func (s *Store) UpdateCookFlags(ctx context.Context, id int64, habit bool, deducted int) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE cooklog SET habit_marked = ?, pantry_deducted = ? WHERE id = ?`, boolInt(habit), deducted, id)
	return err
}

// ListCooks returns the latest cook-log rows (optionally for one recipe).
func (s *Store) ListCooks(ctx context.Context, recipeID int64, limit int) ([]Cook, error) {
	q := `SELECT ` + cookCols + ` FROM cooklog c LEFT JOIN recipes r ON r.id = c.recipe_id LEFT JOIN users u ON u.id = c.user_id`
	args := []any{}
	if recipeID > 0 {
		q += ` WHERE c.recipe_id = ?`
		args = append(args, recipeID)
	}
	q += ` ORDER BY c.date DESC, c.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Cook{}
	for rows.Next() {
		c, err := scanCook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// DeleteCook removes a cook-log row and decrements the recipe counter.
func (s *Store) DeleteCook(ctx context.Context, id int64) error {
	var rid *int64
	if err := s.DB.QueryRowContext(ctx, `SELECT recipe_id FROM cooklog WHERE id = ?`, id).Scan(&rid); err != nil {
		return notFound(err)
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM cooklog WHERE id = ?`, id); err != nil {
		return err
	}
	if rid != nil {
		_, _ = s.DB.ExecContext(ctx, `UPDATE recipes SET times_cooked = MAX(times_cooked - 1, 0) WHERE id = ?`, *rid)
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE plan SET cooked_at = '', cook_id = NULL WHERE cook_id = ?`, id)
	return nil
}

// CookStats returns cooks this month / this year and the streak of days cooked.
func (s *Store) CookStats(ctx context.Context) (month, year int, days30 int) {
	today := Today()
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM cooklog WHERE date >= ?`, today[:7]+"-01").Scan(&month)
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM cooklog WHERE date >= ?`, today[:4]+"-01-01").Scan(&year)
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(DISTINCT date) FROM cooklog WHERE date >= ?`, AddDays(today, -29)).Scan(&days30)
	return
}

// CreatePurchase inserts a checkout.
func (s *Store) CreatePurchase(ctx context.Context, p *Purchase) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO purchases (list_id, list_name, date, store, amount, currency, items, item_names, paid_by, split_group_id, split_expense_id, pantry_restocked, notes, user_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullI(p.ListID), p.ListName, p.Date, p.Store, p.Amount, p.Currency, p.Items, p.ItemNames, p.PaidBy, p.SplitGroupID, p.SplitExpenseID, p.PantryRestocked, p.Notes, nullI(p.UserID))
	if err != nil {
		return err
	}
	p.ID, _ = res.LastInsertId()
	return nil
}

// ListPurchases returns the latest checkouts.
func (s *Store) ListPurchases(ctx context.Context, limit int) ([]Purchase, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, list_id, list_name, date, store, amount, currency, items, item_names, paid_by, split_group_id, split_expense_id, pantry_restocked, notes, user_id, created_at FROM purchases ORDER BY date DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Purchase{}
	for rows.Next() {
		var p Purchase
		if err := rows.Scan(&p.ID, &p.ListID, &p.ListName, &p.Date, &p.Store, &p.Amount, &p.Currency, &p.Items, &p.ItemNames, &p.PaidBy, &p.SplitGroupID, &p.SplitExpenseID, &p.PantryRestocked, &p.Notes, &p.UserID, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SpendStats sums purchases this month and this year (in whatever currency they were saved; the caller converts).
func (s *Store) SpendStats(ctx context.Context) (month, year int64, cur string) {
	today := Today()
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount), 0), COALESCE(MAX(currency), '') FROM purchases WHERE date >= ?`, today[:7]+"-01").Scan(&month, &cur)
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount), 0) FROM purchases WHERE date >= ?`, today[:4]+"-01-01").Scan(&year)
	return
}

// DeletePurchase removes a checkout record (the pantry and Split stay as they are).
func (s *Store) DeletePurchase(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM purchases WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
