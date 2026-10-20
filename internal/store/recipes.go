package store

import (
	"context"
	"encoding/json"
	"strings"
)

const recipeCols = `id, title, description, servings, prep_minutes, cook_minutes, course, cuisine, tags, source_url, source_name, rating, favorite, notes, photo_path, image_url, ingredients, steps, nutrition, times_cooked, last_cooked, archived, created_by, created_at, updated_at`

func scanRecipe(row interface{ Scan(...any) error }) (*Recipe, error) {
	var r Recipe
	var fav, arch int
	var ing, steps, nut string
	if err := row.Scan(&r.ID, &r.Title, &r.Description, &r.Servings, &r.PrepMinutes, &r.CookMinutes, &r.Course, &r.Cuisine, &r.Tags, &r.SourceURL, &r.SourceName, &r.Rating, &fav, &r.Notes, &r.PhotoPath, &r.ImageURL, &ing, &steps, &nut, &r.TimesCooked, &r.LastCooked, &arch, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	r.Favorite, r.Archived, r.HasPhoto = fav == 1, arch == 1, r.PhotoPath != ""
	_ = json.Unmarshal([]byte(ing), &r.Ingredients)
	_ = json.Unmarshal([]byte(steps), &r.Steps)
	_ = json.Unmarshal([]byte(nut), &r.Nutrition)
	if r.Ingredients == nil {
		r.Ingredients = []Ingredient{}
	}
	if r.Steps == nil {
		r.Steps = []string{}
	}
	if r.Nutrition == nil {
		r.Nutrition = map[string]string{}
	}
	r.TotalMinutes = r.PrepMinutes + r.CookMinutes
	return &r, nil
}

// RecipeFilter narrows a recipe list.
type RecipeFilter struct {
	Query    string
	Course   string
	Tag      string
	Cuisine  string
	Favorite bool
	Archived bool
	Sort     string // title | recent | cooked | rating | time
	Limit    int
}

// ListRecipes returns recipes matching the filter.
func (s *Store) ListRecipes(ctx context.Context, uid int64, f RecipeFilter) ([]Recipe, error) {
	where := []string{"user_id = ?", "archived = ?"}
	args := []any{uid, boolInt(f.Archived)}
	if q := strings.TrimSpace(f.Query); q != "" {
		like := "%" + strings.ToLower(q) + "%"
		where = append(where, `(LOWER(title) LIKE ? OR LOWER(description) LIKE ? OR LOWER(tags) LIKE ? OR LOWER(ingredients) LIKE ? OR LOWER(cuisine) LIKE ?)`)
		args = append(args, like, like, like, like, like)
	}
	if f.Course != "" {
		where = append(where, "course = ?")
		args = append(args, f.Course)
	}
	if f.Cuisine != "" {
		where = append(where, "LOWER(cuisine) = ?")
		args = append(args, strings.ToLower(f.Cuisine))
	}
	if f.Tag != "" {
		where = append(where, `(',' || REPLACE(LOWER(tags), ', ', ',') || ',') LIKE ?`)
		args = append(args, "%,"+strings.ToLower(strings.TrimSpace(f.Tag))+",%")
	}
	if f.Favorite {
		where = append(where, "favorite = 1")
	}
	order := "LOWER(title)"
	switch f.Sort {
	case "recent":
		order = "updated_at DESC"
	case "cooked":
		order = "last_cooked DESC, times_cooked DESC, LOWER(title)"
	case "popular":
		order = "times_cooked DESC, LOWER(title)"
	case "rating":
		order = "rating DESC, LOWER(title)"
	case "time":
		order = "(prep_minutes + cook_minutes), LOWER(title)"
	case "new":
		order = "created_at DESC"
	}
	q := `SELECT ` + recipeCols + ` FROM recipes WHERE ` + strings.Join(where, " AND ") + ` ORDER BY ` + order
	if f.Limit > 0 {
		q += ` LIMIT ` + itoa(f.Limit)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Recipe{}
	for rows.Next() {
		r, err := scanRecipe(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// GetRecipe returns one of the user's recipes.
func (s *Store) GetRecipe(ctx context.Context, uid, id int64) (*Recipe, error) {
	return scanRecipe(s.DB.QueryRowContext(ctx, `SELECT `+recipeCols+` FROM recipes WHERE id = ? AND user_id = ?`, id, uid))
}

func jsonStr(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// CreateRecipe inserts a recipe owned by uid.
func (s *Store) CreateRecipe(ctx context.Context, uid int64, r *Recipe) error {
	if uid <= 0 {
		return ErrNotFound
	}
	if r.Ingredients == nil {
		r.Ingredients = []Ingredient{}
	}
	if r.Steps == nil {
		r.Steps = []string{}
	}
	if r.Nutrition == nil {
		r.Nutrition = map[string]string{}
	}
	var err error
	r.ID, err = reserveResourceID(ctx, s.DB, "recipes")
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO recipes (id, title, description, servings, prep_minutes, cook_minutes, course, cuisine, tags, source_url, source_name, rating, favorite, notes, photo_path, image_url, ingredients, steps, nutrition, times_cooked, last_cooked, archived, created_by, user_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, r.ID,
		r.Title, r.Description, r.Servings, r.PrepMinutes, r.CookMinutes, r.Course, r.Cuisine, r.Tags, r.SourceURL, r.SourceName, r.Rating, boolInt(r.Favorite), r.Notes, r.PhotoPath, r.ImageURL,
		jsonStr(r.Ingredients), jsonStr(r.Steps), jsonStr(r.Nutrition), r.TimesCooked, r.LastCooked, boolInt(r.Archived), nullI(r.CreatedBy), uid)
	if err != nil {
		return err
	}
	nr, err := s.GetRecipe(ctx, uid, r.ID)
	if err == nil {
		*r = *nr
	}
	return nil
}

// UpdateRecipe saves every field.
func (s *Store) UpdateRecipe(ctx context.Context, uid int64, r *Recipe) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE recipes SET title = ?, description = ?, servings = ?, prep_minutes = ?, cook_minutes = ?, course = ?, cuisine = ?, tags = ?, source_url = ?, source_name = ?, rating = ?, favorite = ?, notes = ?, photo_path = ?, image_url = ?, ingredients = ?, steps = ?, nutrition = ?, times_cooked = ?, last_cooked = ?, archived = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		r.Title, r.Description, r.Servings, r.PrepMinutes, r.CookMinutes, r.Course, r.Cuisine, r.Tags, r.SourceURL, r.SourceName, r.Rating, boolInt(r.Favorite), r.Notes, r.PhotoPath, r.ImageURL,
		jsonStr(r.Ingredients), jsonStr(r.Steps), jsonStr(r.Nutrition), r.TimesCooked, r.LastCooked, boolInt(r.Archived), Now(), r.ID, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	nr, err := s.GetRecipe(ctx, uid, r.ID)
	if err == nil {
		*r = *nr
	}
	return nil
}

// DeleteRecipe removes a recipe.
func (s *Store) DeleteRecipe(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM recipes WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkCooked bumps the counter and last-cooked date.
func (s *Store) MarkCooked(ctx context.Context, uid, id int64, date string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE recipes SET times_cooked = times_cooked + 1, last_cooked = CASE WHEN last_cooked < ? THEN ? ELSE last_cooked END, updated_at = ? WHERE id = ? AND user_id = ?`, date, date, Now(), id, uid)
	return err
}

// RecipePhotoPaths returns the stored photo paths of a user's recipes (to clean up files).
func (s *Store) RecipePhotoPaths(ctx context.Context, uid int64) []string {
	rows, err := s.DB.QueryContext(ctx, `SELECT photo_path FROM recipes WHERE user_id = ? AND photo_path <> ''`, uid)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if rows.Scan(&p) == nil {
			out = append(out, p)
		}
	}
	return out
}

// RecipeFacets returns the distinct tags, cuisines and courses in use.
func (s *Store) RecipeFacets(ctx context.Context, uid int64) (tags, cuisines []string, courses map[string]int, err error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT tags, cuisine, course FROM recipes WHERE user_id = ? AND archived = 0`, uid)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	tagSet, cuiSet := map[string]bool{}, map[string]bool{}
	courses = map[string]int{}
	for rows.Next() {
		var t, c, co string
		if err := rows.Scan(&t, &c, &co); err != nil {
			return nil, nil, nil, err
		}
		for _, x := range strings.Split(t, ",") {
			if x = strings.TrimSpace(x); x != "" {
				tagSet[strings.ToLower(x)] = true
			}
		}
		if c = strings.TrimSpace(c); c != "" {
			cuiSet[c] = true
		}
		courses[co]++
	}
	for t := range tagSet {
		tags = append(tags, t)
	}
	for c := range cuiSet {
		cuisines = append(cuisines, c)
	}
	sortStrings(tags)
	sortStrings(cuisines)
	return tags, cuisines, courses, rows.Err()
}

// ---- small helpers (kept local so the package has no sort/strconv noise elsewhere) ----

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func sortStrings(a []string) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && strings.ToLower(a[j]) < strings.ToLower(a[j-1]); j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}
