package recipeimport

import "testing"

func TestParseLine(t *testing.T) {
	cases := []struct {
		in         string
		qty        float64
		unit, name string
	}{
		{"1 1/2 cups flour, sifted", 1.5, "cups", "flour"},
		{"200 g sugar", 200, "g", "sugar"},
		{"200g sugar", 200, "g", "sugar"},
		{"2 eggs", 2, "", "eggs"},
		{"½ tsp salt", 0.5, "tsp", "salt"},
		{"3 cloves garlic (minced)", 3, "cloves", "garlic"},
		{"Salt to taste", 0, "", "Salt to taste"},
		{"1/2 onion", 0.5, "", "onion"},
		{"2 tbsp of olive oil", 2, "tbsp", "olive oil"},
	}
	for _, c := range cases {
		p := ParseLine(c.in)
		if p.Qty < c.qty-0.001 || p.Qty > c.qty+0.001 || p.Unit != c.unit || p.Name != c.name {
			t.Errorf("%q → %+v, want qty=%v unit=%q name=%q", c.in, p, c.qty, c.unit, c.name)
		}
	}
}

func TestParseJSONLD(t *testing.T) {
	page := `<html><head><script type="application/ld+json">{"@context":"https://schema.org","@graph":[{"@type":"WebPage"},{"@type":["Recipe"],"name":"Pancakes","recipeYield":"4 servings","prepTime":"PT10M","cookTime":"PT20M","recipeIngredient":["200 g flour","2 eggs","300 ml milk"],"recipeInstructions":[{"@type":"HowToStep","text":"Mix."},{"@type":"HowToSection","itemListElement":[{"@type":"HowToStep","text":"Fry."}]}],"recipeCategory":"Breakfast","keywords":"quick, easy","image":{"url":"https://x/y.jpg"},"nutrition":{"@type":"NutritionInformation","calories":"300 kcal"}}]}</script></head></html>`
	r, err := Parse(page, "https://example.com/pancakes")
	if err != nil {
		t.Fatal(err)
	}
	if r.Title != "Pancakes" || r.Servings != 4 || r.PrepMinutes != 10 || r.CookMinutes != 20 || len(r.Ingredients) != 3 || len(r.Steps) != 2 || r.Course != "breakfast" || r.ImageURL != "https://x/y.jpg" || r.Nutrition["calories"] != "300 kcal" || r.SourceName != "example.com" {
		t.Errorf("unexpected: %+v", r)
	}
}
