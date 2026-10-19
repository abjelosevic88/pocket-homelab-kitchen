package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/recipeimport"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/store"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/units"
)

func recipeFilter(r *http.Request) store.RecipeFilter {
	q := r.URL.Query()
	return store.RecipeFilter{Query: q.Get("q"), Course: q.Get("course"), Tag: q.Get("tag"), Cuisine: q.Get("cuisine"), Favorite: queryBool(r, "favorite"), Archived: queryBool(r, "archived"), Sort: q.Get("sort"), Limit: queryInt(r, "limit")}
}

func (s *Server) handleListRecipes(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListRecipes(r.Context(), actorID(r.Context()), recipeFilter(r))
	if err != nil {
		s.fail(w, err, "list recipes")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleRecipeFacets(w http.ResponseWriter, r *http.Request) {
	tags, cuisines, courses, err := s.store.RecipeFacets(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "facets")
		return
	}
	if tags == nil {
		tags = []string{}
	}
	if cuisines == nil {
		cuisines = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags, "cuisines": cuisines, "courses": courses})
}

// recipeInput is the body of POST/PUT recipes.
type recipeInput struct {
	Title       *string             `json:"title"`
	Description *string             `json:"description"`
	Servings    *int                `json:"servings"`
	PrepMinutes *int                `json:"prep_minutes"`
	CookMinutes *int                `json:"cook_minutes"`
	Course      *string             `json:"course"`
	Cuisine     *string             `json:"cuisine"`
	Tags        *string             `json:"tags"`
	SourceURL   *string             `json:"source_url"`
	SourceName  *string             `json:"source_name"`
	Rating      *int                `json:"rating"`
	Favorite    *bool               `json:"favorite"`
	Notes       *string             `json:"notes"`
	ImageURL    *string             `json:"image_url"`
	Ingredients *[]store.Ingredient `json:"ingredients"`
	Steps       *[]string           `json:"steps"`
	Nutrition   *map[string]string  `json:"nutrition"`
	Archived    *bool               `json:"archived"`
}

func (in recipeInput) apply(rc *store.Recipe) string {
	if in.Title != nil {
		rc.Title = strings.TrimSpace(*in.Title)
	}
	if rc.Title == "" {
		return "a title is required"
	}
	if in.Description != nil {
		rc.Description = strings.TrimSpace(*in.Description)
	}
	if in.Servings != nil {
		rc.Servings = *in.Servings
	}
	if rc.Servings <= 0 {
		rc.Servings = 1
	}
	if in.PrepMinutes != nil {
		rc.PrepMinutes = max(0, *in.PrepMinutes)
	}
	if in.CookMinutes != nil {
		rc.CookMinutes = max(0, *in.CookMinutes)
	}
	if in.Course != nil {
		c := strings.ToLower(strings.TrimSpace(*in.Course))
		if c == "" {
			c = "dinner"
		}
		if !store.ValidCourse(c) {
			return "unknown course " + c
		}
		rc.Course = c
	}
	if in.Cuisine != nil {
		rc.Cuisine = strings.TrimSpace(*in.Cuisine)
	}
	if in.Tags != nil {
		rc.Tags = normTags(*in.Tags)
	}
	if in.SourceURL != nil {
		rc.SourceURL = strings.TrimSpace(*in.SourceURL)
	}
	if in.SourceName != nil {
		rc.SourceName = strings.TrimSpace(*in.SourceName)
	}
	if in.Rating != nil {
		rc.Rating = min(5, max(0, *in.Rating))
	}
	if in.Favorite != nil {
		rc.Favorite = *in.Favorite
	}
	if in.Notes != nil {
		rc.Notes = strings.TrimSpace(*in.Notes)
	}
	if in.ImageURL != nil {
		rc.ImageURL = strings.TrimSpace(*in.ImageURL)
	}
	if in.Ingredients != nil {
		out := make([]store.Ingredient, 0, len(*in.Ingredients))
		for _, ing := range *in.Ingredients {
			ing.Name = strings.TrimSpace(ing.Name)
			if ing.Name == "" {
				continue
			}
			ing.Unit = units.Normalize(ing.Unit)
			ing.Note = strings.TrimSpace(ing.Note)
			ing.Group = strings.TrimSpace(ing.Group)
			if ing.Qty < 0 {
				ing.Qty = 0
			}
			out = append(out, ing)
		}
		rc.Ingredients = out
	}
	if in.Steps != nil {
		out := make([]string, 0, len(*in.Steps))
		for _, st := range *in.Steps {
			if st = strings.TrimSpace(st); st != "" {
				out = append(out, st)
			}
		}
		rc.Steps = out
	}
	if in.Nutrition != nil {
		out := map[string]string{}
		for k, v := range *in.Nutrition {
			if k = strings.TrimSpace(k); k != "" && strings.TrimSpace(v) != "" {
				out[k] = strings.TrimSpace(v)
			}
		}
		rc.Nutrition = out
	}
	if in.Archived != nil {
		rc.Archived = *in.Archived
	}
	return ""
}

func normTags(t string) string {
	seen := map[string]bool{}
	var out []string
	for _, x := range strings.FieldsFunc(t, func(r rune) bool { return r == ',' || r == ';' }) {
		x = strings.ToLower(strings.TrimSpace(x))
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return strings.Join(out, ", ")
}

func (s *Server) handleCreateRecipe(w http.ResponseWriter, r *http.Request) {
	var in recipeInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st, _ := s.store.GetSettings(r.Context(), actorID(r.Context()))
	rc := &store.Recipe{Servings: st.DefaultServings, Course: "dinner"}
	uid := actorID(r.Context())
	if uid > 0 {
		rc.CreatedBy = &uid
	}
	if msg := in.apply(rc); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	s.linkPantry(r, rc)
	if err := s.store.CreateRecipe(r.Context(), actorID(r.Context()), rc); err != nil {
		s.fail(w, err, "create recipe")
		return
	}
	writeJSON(w, http.StatusCreated, rc)
}

// linkPantry fills pantry_id on ingredients whose name matches a pantry row.
func (s *Server) linkPantry(r *http.Request, rc *store.Recipe) {
	for i := range rc.Ingredients {
		if rc.Ingredients[i].PantryID != 0 {
			if _, err := s.store.GetPantry(r.Context(), actorID(r.Context()), rc.Ingredients[i].PantryID); err == nil {
				continue
			}
			rc.Ingredients[i].PantryID = 0
		}
		if p, err := s.store.FindPantryByName(r.Context(), actorID(r.Context()), rc.Ingredients[i].Name); err == nil {
			rc.Ingredients[i].PantryID = p.ID
		}
	}
}

func (s *Server) handleGetRecipe(w http.ResponseWriter, r *http.Request) {
	rc, err := s.store.GetRecipe(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get recipe")
		return
	}
	cooks, _ := s.store.ListCooks(r.Context(), actorID(r.Context()), rc.ID, 10)
	dates := s.store.PlanDatesForRecipe(r.Context(), actorID(r.Context()), rc.ID)
	if dates == nil {
		dates = []string{}
	}
	rc.Planned = len(dates)
	pantry := s.pantryMatches(r, rc)
	in := 0
	for _, m := range pantry {
		if m != nil && m.Enough {
			in++
		}
	}
	rc.InPantry = in
	writeJSON(w, http.StatusOK, map[string]any{"recipe": rc, "cooks": cooks, "planned": dates, "pantry": pantry})
}

// pantryMatch says what the pantry holds for one ingredient line.
type pantryMatch struct {
	PantryID  int64   `json:"pantry_id"`
	Name      string  `json:"name"`
	Quantity  float64 `json:"quantity"`
	Unit      string  `json:"unit"`
	Needed    float64 `json:"needed"` // in the pantry unit when convertible
	Enough    bool    `json:"enough"`
	Converted bool    `json:"converted"`
}

func (s *Server) pantryMatches(r *http.Request, rc *store.Recipe) []*pantryMatch {
	out := make([]*pantryMatch, len(rc.Ingredients))
	for i, ing := range rc.Ingredients {
		var p *store.PantryItem
		var err error
		if ing.PantryID > 0 {
			p, err = s.store.GetPantry(r.Context(), actorID(r.Context()), ing.PantryID)
		}
		if p == nil || err != nil {
			p, err = s.store.FindPantryByName(r.Context(), actorID(r.Context()), ing.Name)
		}
		if err != nil || p == nil || p.Archived {
			continue
		}
		m := &pantryMatch{PantryID: p.ID, Name: p.Name, Quantity: p.Quantity, Unit: p.Unit}
		if ing.Qty > 0 {
			if need, ok := units.Convert(ing.Qty, ing.Unit, p.Unit); ok {
				m.Needed, m.Converted = need, true
				m.Enough = p.Quantity >= need-1e-9
			} else {
				m.Enough = p.Quantity > 0
			}
		} else {
			m.Enough = p.Quantity > 0
		}
		out[i] = m
	}
	return out
}

func (s *Server) handleUpdateRecipe(w http.ResponseWriter, r *http.Request) {
	rc, err := s.store.GetRecipe(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get recipe")
		return
	}
	var in recipeInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if msg := in.apply(rc); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if in.Ingredients != nil {
		s.linkPantry(r, rc)
	}
	if err := s.store.UpdateRecipe(r.Context(), actorID(r.Context()), rc); err != nil {
		s.fail(w, err, "update recipe")
		return
	}
	writeJSON(w, http.StatusOK, rc)
}

func (s *Server) handleDeleteRecipe(w http.ResponseWriter, r *http.Request) {
	rc, err := s.store.GetRecipe(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get recipe")
		return
	}
	if err := s.store.DeleteRecipe(r.Context(), actorID(r.Context()), rc.ID); err != nil {
		s.fail(w, err, "delete recipe")
		return
	}
	s.removeFile(rc.PhotoPath)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleFavoriteRecipe(w http.ResponseWriter, r *http.Request) {
	rc, err := s.store.GetRecipe(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get recipe")
		return
	}
	rc.Favorite = !rc.Favorite
	if err := s.store.UpdateRecipe(r.Context(), actorID(r.Context()), rc); err != nil {
		s.fail(w, err, "update recipe")
		return
	}
	writeJSON(w, http.StatusOK, rc)
}

func (s *Server) handleDuplicateRecipe(w http.ResponseWriter, r *http.Request) {
	rc, err := s.store.GetRecipe(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get recipe")
		return
	}
	cp := *rc
	cp.ID, cp.Title, cp.PhotoPath, cp.TimesCooked, cp.LastCooked, cp.Favorite = 0, rc.Title+" (copy)", "", 0, "", false
	uid := actorID(r.Context())
	if uid > 0 {
		cp.CreatedBy = &uid
	}
	if err := s.store.CreateRecipe(r.Context(), actorID(r.Context()), &cp); err != nil {
		s.fail(w, err, "duplicate recipe")
		return
	}
	writeJSON(w, http.StatusCreated, cp)
}

func (s *Server) handleRecipePhoto(w http.ResponseWriter, r *http.Request) {
	rc, err := s.store.GetRecipe(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get recipe")
		return
	}
	s.serveFile(w, r, rc.PhotoPath)
}

func (s *Server) handleUploadRecipePhoto(w http.ResponseWriter, r *http.Request) {
	rc, err := s.store.GetRecipe(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get recipe")
		return
	}
	b, name, ct, err := s.readUpload(r, "file")
	if err != nil || len(b) == 0 {
		writeErr(w, http.StatusBadRequest, "no file")
		return
	}
	if !strings.HasPrefix(ct, "image/") {
		writeErr(w, http.StatusBadRequest, "only images, please")
		return
	}
	ext := ".jpg"
	if i := strings.LastIndex(name, "."); i >= 0 {
		ext = name[i:]
	}
	rel, err := s.saveFile("recipes", ext, b)
	if err != nil {
		s.fail(w, err, "save photo")
		return
	}
	s.removeFile(rc.PhotoPath)
	rc.PhotoPath = rel
	if err := s.store.UpdateRecipe(r.Context(), actorID(r.Context()), rc); err != nil {
		s.fail(w, err, "update recipe")
		return
	}
	writeJSON(w, http.StatusOK, rc)
}

func (s *Server) handleDeleteRecipePhoto(w http.ResponseWriter, r *http.Request) {
	rc, err := s.store.GetRecipe(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get recipe")
		return
	}
	s.removeFile(rc.PhotoPath)
	rc.PhotoPath = ""
	if err := s.store.UpdateRecipe(r.Context(), actorID(r.Context()), rc); err != nil {
		s.fail(w, err, "update recipe")
		return
	}
	writeJSON(w, http.StatusOK, rc)
}

// handleImportRecipe fetches a URL and returns the parsed recipe; with
// {"save": true} it is stored right away.
func (s *Server) handleImportRecipe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL  string `json:"url"`
		Save bool   `json:"save"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	imp, err := recipeimport.Fetch(r.Context(), in.URL, s.cfg.UserAgent)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	rc := &store.Recipe{Title: imp.Title, Description: imp.Description, Servings: imp.Servings, PrepMinutes: imp.PrepMinutes, CookMinutes: imp.CookMinutes, Course: imp.Course, Cuisine: imp.Cuisine, Tags: normTags(imp.Tags), SourceURL: imp.SourceURL, SourceName: imp.SourceName, ImageURL: imp.ImageURL, Steps: imp.Steps, Nutrition: imp.Nutrition}
	if rc.Servings <= 0 {
		st, _ := s.store.GetSettings(r.Context(), actorID(r.Context()))
		rc.Servings = st.DefaultServings
	}
	for _, p := range imp.Ingredients {
		rc.Ingredients = append(rc.Ingredients, store.Ingredient{Name: p.Name, Qty: p.Qty, Unit: units.Normalize(p.Unit), Note: p.Note})
	}
	if rc.Ingredients == nil {
		rc.Ingredients = []store.Ingredient{}
	}
	if rc.Steps == nil {
		rc.Steps = []string{}
	}
	s.linkPantry(r, rc)
	if !in.Save {
		writeJSON(w, http.StatusOK, rc)
		return
	}
	uid := actorID(r.Context())
	if uid > 0 {
		rc.CreatedBy = &uid
	}
	if err := s.store.CreateRecipe(r.Context(), actorID(r.Context()), rc); err != nil {
		s.fail(w, err, "create recipe")
		return
	}
	writeJSON(w, http.StatusCreated, rc)
}

// handleRecipeToList adds a recipe's ingredients to a shopping list.
func (s *Server) handleRecipeToList(w http.ResponseWriter, r *http.Request) {
	rc, err := s.store.GetRecipe(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get recipe")
		return
	}
	var in struct {
		ListID     int64 `json:"list_id"`
		Servings   int   `json:"servings"`
		SkipPantry bool  `json:"skip_pantry"`
		PlanID     int64 `json:"plan_id"`
	}
	_ = decode(r, &in)
	list, err := s.resolveList(r, in.ListID)
	if err != nil {
		s.fail(w, err, "list")
		return
	}
	if in.PlanID > 0 {
		if _, err := s.store.GetPlan(r.Context(), actorID(r.Context()), in.PlanID); err != nil {
			in.PlanID = 0
		}
	}
	added, skipped := s.addRecipeToList(r, list.ID, rc, in.Servings, in.SkipPantry, in.PlanID)
	writeJSON(w, http.StatusOK, map[string]any{"list": list, "added": added, "skipped": skipped})
}

func (s *Server) resolveList(r *http.Request, id int64) (*store.List, error) {
	if id > 0 {
		return s.store.GetList(r.Context(), actorID(r.Context()), id)
	}
	return s.store.DefaultList(r.Context(), actorID(r.Context()))
}

// addRecipeToList merges scaled ingredient lines into a list; skipPantry
// drops (or reduces) lines the pantry already covers.
func (s *Server) addRecipeToList(r *http.Request, listID int64, rc *store.Recipe, servings int, skipPantry bool, planID int64) (added []store.ListItem, skipped []string) {
	factor := 1.0
	if servings > 0 && rc.Servings > 0 {
		factor = float64(servings) / float64(rc.Servings)
	}
	uid := actorID(r.Context())
	for _, ing := range rc.Ingredients {
		if ing.Optional {
			continue
		}
		qty := ing.Qty * factor
		var pantryID *int64
		if skipPantry || ing.PantryID > 0 {
			var p *store.PantryItem
			var err error
			if ing.PantryID > 0 {
				p, err = s.store.GetPantry(r.Context(), actorID(r.Context()), ing.PantryID)
			}
			if p == nil || err != nil {
				p, err = s.store.FindPantryByName(r.Context(), actorID(r.Context()), ing.Name)
			}
			if err == nil && p != nil && !p.Archived {
				pid := p.ID
				pantryID = &pid
				if skipPantry {
					if qty <= 0 && p.Quantity > 0 {
						skipped = append(skipped, ing.Name)
						continue
					}
					if need, ok := units.Convert(qty, ing.Unit, p.Unit); ok && qty > 0 {
						if p.Quantity >= need-1e-9 {
							skipped = append(skipped, ing.Name)
							continue
						}
						if p.Quantity > 0 {
							back, _ := units.Convert(need-p.Quantity, p.Unit, ing.Unit)
							qty = back
						}
					}
				}
			}
		}
		it := s.mergeItem(r, listID, ing.Name, qty, ing.Unit, ing.Note, aisleFor(ing.Name), &rc.ID, pidOrNil(planID), pantryID, uid)
		if it != nil {
			added = append(added, *it)
		}
	}
	if added == nil {
		added = []store.ListItem{}
	}
	if skipped == nil {
		skipped = []string{}
	}
	return
}

func pidOrNil(id int64) *int64 {
	if id <= 0 {
		return nil
	}
	return &id
}

// mergeItem adds a line or tops up an existing open line with the same name and unit family.
func (s *Server) mergeItem(r *http.Request, listID int64, name string, qty float64, unit, note, aisle string, recipeID, planID, pantryID *int64, uid int64) *store.ListItem {
	unit = units.Normalize(unit)
	if ex, err := s.store.FindOpenItem(r.Context(), actorID(r.Context()), listID, name, unit); err == nil {
		ex.Qty += qty
		if note != "" && !strings.Contains(ex.Note, note) {
			if ex.Note != "" {
				ex.Note += "; "
			}
			ex.Note += note
		}
		if ex.PantryID == nil {
			ex.PantryID = pantryID
		}
		if err := s.store.UpdateItem(r.Context(), actorID(r.Context()), ex); err == nil {
			return ex
		}
	}
	// same name, convertible unit (g ↔ kg) → merge into the existing line's unit
	if f := units.FamilyOf(unit); f == units.Mass || f == units.Volume {
		for _, cand := range []string{"g", "kg", "ml", "l", "tsp", "tbsp", "cup"} {
			if cand == unit {
				continue
			}
			if ex, err := s.store.FindOpenItem(r.Context(), actorID(r.Context()), listID, name, cand); err == nil {
				if conv, ok := units.Convert(qty, unit, cand); ok {
					ex.Qty += conv
					base, fam := units.ToBase(ex.Qty, ex.Unit)
					ex.Qty, ex.Unit = units.Pretty(base, fam, ex.Unit)
					if ex.PantryID == nil {
						ex.PantryID = pantryID
					}
					if err := s.store.UpdateItem(r.Context(), actorID(r.Context()), ex); err == nil {
						return ex
					}
				}
			}
		}
	}
	it := &store.ListItem{ListID: listID, Name: name, Qty: qty, Unit: unit, Note: note, Aisle: aisle, RecipeID: recipeID, PlanID: planID, PantryID: pantryID}
	if uid > 0 {
		it.CreatedBy = &uid
	}
	if err := s.store.CreateItem(r.Context(), actorID(r.Context()), it); err != nil {
		return nil
	}
	return it
}

var aisleWords = map[string][]string{
	"produce":   {"onion", "garlic", "tomato", "potato", "carrot", "pepper", "lettuce", "spinach", "apple", "banana", "lemon", "lime", "orange", "cucumber", "zucchini", "courgette", "aubergine", "eggplant", "mushroom", "herb", "parsley", "basil", "cilantro", "coriander", "ginger", "celery", "leek", "cabbage", "broccoli", "cauliflower", "avocado", "berry", "berries", "grape", "pear", "peach", "plum", "chili", "chilli", "salad", "kale", "beet", "pumpkin", "squash", "corn", "pea", "bean sprout", "mint", "dill", "scallion", "spring onion"},
	"dairy":     {"milk", "butter", "cheese", "cream", "yogurt", "yoghurt", "egg", "mozzarella", "parmesan", "feta", "ricotta", "mascarpone", "kefir", "kajmak", "sour cream"},
	"meat":      {"chicken", "beef", "pork", "lamb", "bacon", "sausage", "ham", "turkey", "mince", "ground", "steak", "veal", "prosciutto", "salami", "ćevap", "cevap", "pljeskavica"},
	"fish":      {"salmon", "tuna", "cod", "shrimp", "prawn", "fish", "anchov", "sardine", "mussel", "squid", "trout", "mackerel"},
	"bakery":    {"bread", "bun", "roll", "tortilla", "pita", "baguette", "croissant", "wrap", "lepinja", "somun"},
	"frozen":    {"frozen", "ice cream", "peas"},
	"drinks":    {"juice", "wine", "beer", "water", "soda", "cola", "coffee", "tea"},
	"spices":    {"salt", "pepper", "paprika", "cumin", "cinnamon", "oregano", "thyme", "rosemary", "turmeric", "curry", "chili powder", "nutmeg", "vegeta", "spice", "bay leaf", "clove", "cardamom", "vanilla"},
	"household": {"paper", "soap", "detergent", "foil", "bag", "sponge", "cleaner", "toilet"},
}

// aisleFor guesses an aisle from an ingredient name.
func aisleFor(name string) string {
	n := " " + strings.ToLower(name) + " "
	for _, aisle := range []string{"spices", "dairy", "meat", "fish", "bakery", "frozen", "drinks", "household", "produce"} {
		for _, wd := range aisleWords[aisle] {
			// short words must stand alone ("roll" must not hit "rolled oats"); longer ones may be stems
			if len(wd) <= 4 {
				if strings.Contains(n, " "+wd+" ") || strings.Contains(n, " "+wd+"s ") {
					return aisle
				}
			} else if strings.Contains(n, wd) {
				return aisle
			}
		}
	}
	return "pantry"
}

func (s *Server) handleRecipeMarkdown(w http.ResponseWriter, r *http.Request) {
	rc, err := s.store.GetRecipe(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get recipe")
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", rc.Title)
	if rc.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", rc.Description)
	}
	fmt.Fprintf(&b, "Serves %d · prep %d min · cook %d min\n\n", rc.Servings, rc.PrepMinutes, rc.CookMinutes)
	b.WriteString("## Ingredients\n\n")
	for _, ing := range rc.Ingredients {
		line := "- "
		if q := units.Format(ing.Qty, ing.Unit); q != "" {
			line += q + " "
		}
		line += ing.Name
		if ing.Note != "" {
			line += ", " + ing.Note
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n## Steps\n\n")
	for i, st := range rc.Steps {
		fmt.Fprintf(&b, "%d. %s\n", i+1, st)
	}
	if rc.SourceURL != "" {
		fmt.Fprintf(&b, "\nSource: %s\n", rc.SourceURL)
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(rc.Title, `"`, "")+`.md"`)
	_, _ = w.Write([]byte(b.String()))
}

// handleSearch is the one-box search across recipes, pantry and lists.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"recipes": []store.Recipe{}, "pantry": []store.PantryItem{}})
		return
	}
	recipes, _ := s.store.ListRecipes(r.Context(), actorID(r.Context()), store.RecipeFilter{Query: q, Limit: 8})
	pantry, _ := s.store.ListPantry(r.Context(), actorID(r.Context()), store.PantryFilter{Query: q})
	if len(pantry) > 8 {
		pantry = pantry[:8]
	}
	writeJSON(w, http.StatusOK, map[string]any{"recipes": recipes, "pantry": pantry})
}

// cookInput is shared by "cook this recipe" and "cook this plan entry".
type cookInput struct {
	Date         string `json:"date"`
	Servings     int    `json:"servings"`
	Notes        string `json:"notes"`
	Rating       int    `json:"rating"`
	DeductPantry *bool  `json:"deduct_pantry"`
	MarkHabit    *bool  `json:"mark_habit"`
	PlanID       int64  `json:"plan_id"`
}

func (s *Server) handleCookRecipe(w http.ResponseWriter, r *http.Request) {
	rc, err := s.store.GetRecipe(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get recipe")
		return
	}
	var in cookInput
	_ = decode(r, &in)
	res, err := s.cook(r, rc, in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// ---- dates ----

func weekRange(from, to string, weekStart string) (string, string) {
	if from == "" {
		t := time.Now()
		wd := int(t.Weekday()) // Sunday = 0
		off := wd - 1
		if weekStart == "sun" {
			off = wd
		}
		if off < 0 {
			off += 7
		}
		from = t.AddDate(0, 0, -off).Format("2006-01-02")
	}
	if to == "" {
		to = store.AddDays(from, 6)
	}
	return from, to
}
