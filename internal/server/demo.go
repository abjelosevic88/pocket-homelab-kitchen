package server

import (
	"context"

	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/store"
)

// seedDemo fills an empty database with a few recipes, a pantry, this week's
// plan and a shopping list (DEMO_DATA=true) — all of it the owner's. Runs once.
func (s *Server) seedDemo(ctx context.Context, uid int64) error {
	if c := s.store.CountsFor(ctx, uid); c["recipes"] > 0 {
		return nil
	}
	pantry := []store.PantryItem{
		{Name: "Flour", Quantity: 1500, Unit: "g", MinQuantity: 500, Location: "Pantry", Category: "baking"},
		{Name: "Sugar", Quantity: 800, Unit: "g", MinQuantity: 300, Location: "Pantry", Category: "baking"},
		{Name: "Eggs", Quantity: 6, Unit: "pcs", MinQuantity: 4, Location: "Fridge", Category: "dairy"},
		{Name: "Milk", Quantity: 1000, Unit: "ml", MinQuantity: 500, Location: "Fridge", Category: "dairy", ExpiryDate: store.AddDays(store.Today(), 3)},
		{Name: "Butter", Quantity: 250, Unit: "g", MinQuantity: 100, Location: "Fridge", Category: "dairy"},
		{Name: "Olive oil", Quantity: 500, Unit: "ml", MinQuantity: 200, Location: "Pantry", Category: "oils"},
		{Name: "Spaghetti", Quantity: 500, Unit: "g", MinQuantity: 500, Location: "Pantry", Category: "pasta"},
		{Name: "Rice", Quantity: 2000, Unit: "g", MinQuantity: 500, Location: "Pantry", Category: "grains"},
		{Name: "Canned tomatoes", Quantity: 3, Unit: "can", MinQuantity: 2, Location: "Pantry", Category: "canned"},
		{Name: "Garlic", Quantity: 8, Unit: "clove", MinQuantity: 3, Location: "Pantry", Category: "produce"},
		{Name: "Onion", Quantity: 4, Unit: "pcs", MinQuantity: 2, Location: "Pantry", Category: "produce"},
		{Name: "Parmesan", Quantity: 150, Unit: "g", MinQuantity: 50, Location: "Fridge", Category: "dairy"},
		{Name: "Chicken thighs", Quantity: 600, Unit: "g", Location: "Freezer", Category: "meat"},
		{Name: "Salt", Quantity: 900, Unit: "g", MinQuantity: 200, Location: "Pantry", Category: "spices"},
		{Name: "Black pepper", Quantity: 40, Unit: "g", MinQuantity: 10, Location: "Pantry", Category: "spices"},
		{Name: "Paprika", Quantity: 60, Unit: "g", MinQuantity: 20, Location: "Pantry", Category: "spices"},
		{Name: "Yogurt", Quantity: 400, Unit: "g", Location: "Fridge", Category: "dairy", ExpiryDate: store.AddDays(store.Today(), -1)},
		{Name: "Pancetta", Quantity: 0, Unit: "g", MinQuantity: 100, Location: "Fridge", Category: "meat"},
	}
	ids := map[string]int64{}
	for i := range pantry {
		if err := s.store.CreatePantry(ctx, uid, &pantry[i]); err != nil {
			return err
		}
		ids[pantry[i].Name] = pantry[i].ID
	}
	ing := func(name string, qty float64, unit, note string) store.Ingredient {
		return store.Ingredient{Name: name, Qty: qty, Unit: unit, Note: note, PantryID: ids[name]}
	}
	recipes := []store.Recipe{
		{Title: "Spaghetti carbonara", Description: "The Roman classic: eggs, pecorino, pancetta, pepper. No cream.", Servings: 2, PrepMinutes: 10, CookMinutes: 15, Course: "dinner", Cuisine: "Italian", Tags: "pasta, quick, classic", Rating: 5, Favorite: true, TimesCooked: 7, LastCooked: store.AddDays(store.Today(), -12),
			Ingredients: []store.Ingredient{ing("Spaghetti", 200, "g", ""), ing("Pancetta", 120, "g", "diced"), ing("Eggs", 2, "pcs", "plus one yolk"), ing("Parmesan", 60, "g", "or pecorino, grated"), ing("Black pepper", 2, "g", "freshly ground"), ing("Salt", 5, "g", "for the water")},
			Steps:       []string{"Boil salted water and cook the spaghetti al dente.", "Fry the pancetta slowly until the fat renders and it crisps.", "Whisk the eggs with the cheese and plenty of pepper.", "Drain the pasta, keep a cup of water; toss with the pancetta off the heat, then stir in the egg mixture, loosening with pasta water until glossy."}},
		{Title: "Tomato & garlic pasta", Description: "Pantry dinner in twenty minutes.", Servings: 2, PrepMinutes: 5, CookMinutes: 20, Course: "dinner", Cuisine: "Italian", Tags: "pasta, vegetarian, pantry", Rating: 4, TimesCooked: 11, LastCooked: store.AddDays(store.Today(), -5),
			Ingredients: []store.Ingredient{ing("Spaghetti", 200, "g", ""), ing("Canned tomatoes", 1, "can", ""), ing("Garlic", 3, "clove", "sliced"), ing("Olive oil", 3, "tbsp", ""), ing("Onion", 1, "pcs", "finely chopped"), ing("Parmesan", 30, "g", ""), ing("Salt", 0, "", "to taste")},
			Steps:       []string{"Soften the onion in olive oil, add the garlic for a minute.", "Add the tomatoes, crush them, simmer 15 minutes with salt.", "Toss with the cooked pasta and finish with parmesan."}},
		{Title: "Pancakes", Description: "Sunday morning stack.", Servings: 4, PrepMinutes: 10, CookMinutes: 15, Course: "breakfast", Cuisine: "American", Tags: "sweet, kids, weekend", Rating: 4, Favorite: true, TimesCooked: 15, LastCooked: store.AddDays(store.Today(), -2),
			Ingredients: []store.Ingredient{ing("Flour", 200, "g", ""), ing("Milk", 300, "ml", ""), ing("Eggs", 2, "pcs", ""), ing("Sugar", 30, "g", ""), ing("Butter", 30, "g", "melted"), {Name: "Baking powder", Qty: 2, Unit: "tsp"}, ing("Salt", 0, "", "a pinch")},
			Steps:       []string{"Whisk the dry ingredients.", "Whisk milk, eggs and melted butter; fold into the dry mix, lumps are fine.", "Cook ladlefuls on a medium pan until bubbles form, flip, one more minute."}},
		{Title: "Chicken paprikash", Description: "Slow, red and comforting.", Servings: 4, PrepMinutes: 15, CookMinutes: 60, Course: "dinner", Cuisine: "Hungarian", Tags: "stew, comfort, batch", Rating: 5, TimesCooked: 4, LastCooked: store.AddDays(store.Today(), -30),
			Ingredients: []store.Ingredient{ing("Chicken thighs", 800, "g", "skin on"), ing("Onion", 2, "pcs", "sliced"), ing("Paprika", 2, "tbsp", "sweet"), ing("Canned tomatoes", 1, "can", ""), {Name: "Sour cream", Qty: 200, Unit: "g"}, ing("Flour", 1, "tbsp", ""), ing("Olive oil", 2, "tbsp", ""), ing("Salt", 0, "", ""), ing("Black pepper", 0, "", ""), {Name: "Bell pepper", Qty: 1, Unit: "pcs", Optional: true}},
			Steps:       []string{"Brown the chicken, set aside.", "Soften the onions, add the paprika off the heat so it does not burn.", "Return the chicken, add tomatoes and a splash of water, simmer 45 minutes.", "Stir the flour into the sour cream, fold in, warm through without boiling."}},
		{Title: "Fried rice", Description: "Yesterday's rice, today's dinner.", Servings: 2, PrepMinutes: 10, CookMinutes: 10, Course: "dinner", Cuisine: "Chinese", Tags: "quick, leftovers, rice", Rating: 3, TimesCooked: 9, LastCooked: store.AddDays(store.Today(), -8),
			Ingredients: []store.Ingredient{ing("Rice", 300, "g", "cooked and cold"), ing("Eggs", 2, "pcs", ""), ing("Garlic", 2, "clove", ""), {Name: "Spring onion", Qty: 3, Unit: "pcs"}, {Name: "Soy sauce", Qty: 2, Unit: "tbsp"}, {Name: "Frozen peas", Qty: 100, Unit: "g"}, ing("Olive oil", 2, "tbsp", "")},
			Steps:       []string{"Scramble the eggs, set aside.", "Fry garlic and the white of the onions, add the rice and peas on high heat.", "Season with soy, fold in the eggs and greens."}},
		{Title: "Overnight oats", Description: "No cooking at all.", Servings: 1, PrepMinutes: 5, CookMinutes: 0, Course: "breakfast", Cuisine: "", Tags: "healthy, no-cook, prep", Rating: 3, TimesCooked: 20, LastCooked: store.AddDays(store.Today(), -1),
			Ingredients: []store.Ingredient{{Name: "Rolled oats", Qty: 50, Unit: "g"}, ing("Milk", 120, "ml", ""), ing("Yogurt", 80, "g", ""), {Name: "Honey", Qty: 1, Unit: "tsp"}, {Name: "Banana", Qty: 1, Unit: "pcs", Optional: true}},
			Steps:       []string{"Stir everything in a jar, fridge overnight, top with fruit."}},
		{Title: "Shakshuka", Description: "Eggs poached in spiced tomato.", Servings: 2, PrepMinutes: 10, CookMinutes: 25, Course: "lunch", Cuisine: "Middle Eastern", Tags: "eggs, vegetarian, one-pan", Rating: 4, TimesCooked: 3, LastCooked: store.AddDays(store.Today(), -45),
			Ingredients: []store.Ingredient{ing("Canned tomatoes", 1, "can", ""), ing("Eggs", 4, "pcs", ""), ing("Onion", 1, "pcs", ""), ing("Garlic", 2, "clove", ""), ing("Paprika", 1, "tsp", ""), {Name: "Cumin", Qty: 1, Unit: "tsp"}, ing("Olive oil", 2, "tbsp", ""), {Name: "Feta", Qty: 50, Unit: "g", Optional: true}},
			Steps:       []string{"Soften onion and garlic with the spices.", "Add the tomatoes and simmer until thick.", "Make wells, crack in the eggs, cover and cook until just set."}},
		{Title: "Banana bread", Description: "For the three bananas nobody ate.", Servings: 8, PrepMinutes: 15, CookMinutes: 55, Course: "dessert", Cuisine: "", Tags: "baking, sweet", Rating: 4, TimesCooked: 2, LastCooked: "",
			Ingredients: []store.Ingredient{{Name: "Banana", Qty: 3, Unit: "pcs", Note: "very ripe"}, ing("Flour", 250, "g", ""), ing("Sugar", 150, "g", ""), ing("Butter", 100, "g", "melted"), ing("Eggs", 1, "pcs", ""), {Name: "Baking soda", Qty: 1, Unit: "tsp"}, ing("Salt", 0, "", "a pinch")},
			Steps:       []string{"Mash the bananas with the melted butter.", "Mix in sugar, egg, then flour, soda and salt.", "Bake at 175 °C for 55 minutes in a loaf tin."}},
	}
	rids := map[string]int64{}
	for i := range recipes {
		if err := s.store.CreateRecipe(ctx, uid, &recipes[i]); err != nil {
			return err
		}
		rids[recipes[i].Title] = recipes[i].ID
	}
	today := store.Today()
	plan := []struct {
		d     int
		slot  string
		title string
	}{
		{0, "breakfast", "Overnight oats"}, {0, "dinner", "Tomato & garlic pasta"},
		{1, "dinner", "Chicken paprikash"}, {2, "lunch", "Shakshuka"}, {2, "dinner", "Fried rice"},
		{3, "dinner", "Spaghetti carbonara"}, {4, "dinner", ""}, {5, "breakfast", "Pancakes"}, {5, "dinner", "Chicken paprikash"},
		{6, "dinner", "Banana bread"},
	}
	for _, p := range plan {
		e := store.PlanEntry{Date: store.AddDays(today, p.d), Slot: p.slot}
		if p.title == "" {
			e.Title = "Eating out"
		} else {
			id := rids[p.title]
			e.RecipeID = &id
		}
		if err := s.store.CreatePlan(ctx, uid, &e); err != nil {
			return err
		}
	}
	l, err := s.store.DefaultList(ctx, uid)
	if err != nil {
		return err
	}
	for _, it := range []store.ListItem{
		{Name: "Pancetta", Qty: 150, Unit: "g", Aisle: "meat"}, {Name: "Sour cream", Qty: 200, Unit: "g", Aisle: "dairy"}, {Name: "Banana", Qty: 6, Unit: "pcs", Aisle: "produce"},
		{Name: "Spring onion", Qty: 1, Unit: "bunch", Aisle: "produce"}, {Name: "Soy sauce", Qty: 1, Unit: "pcs", Aisle: "pantry"}, {Name: "Dish soap", Qty: 1, Unit: "pcs", Aisle: "household", Checked: true, Price: 349, Currency: "EUR"},
	} {
		it.ListID = l.ID
		if err := s.store.CreateItem(ctx, uid, &it); err != nil {
			return err
		}
	}
	for _, c := range []store.Cook{{Date: store.AddDays(today, -1), Servings: 1}, {Date: store.AddDays(today, -2), Servings: 4}, {Date: store.AddDays(today, -5), Servings: 2}} {
		switch c.Date {
		case store.AddDays(today, -1):
			id := rids["Overnight oats"]
			c.RecipeID = &id
		case store.AddDays(today, -2):
			id := rids["Pancakes"]
			c.RecipeID = &id
		default:
			id := rids["Tomato & garlic pasta"]
			c.RecipeID = &id
		}
		_ = s.store.CreateCook(ctx, uid, &c)
	}
	_ = s.store.CreatePurchase(ctx, uid, &store.Purchase{ListName: l.Name, Date: store.AddDays(today, -3), Store: "Bingo", Amount: 4720, Currency: "EUR", Items: 9, ItemNames: "milk, eggs, flour, onions, tomatoes, rice, butter, parmesan, yogurt", PantryRestocked: 9})
	s.log.Info("demo data seeded")
	return nil
}
