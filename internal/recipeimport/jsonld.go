// Package recipeimport pulls a recipe out of a web page: schema.org Recipe
// JSON-LD first (what nearly every recipe site publishes), with a plain
// heuristic fallback for pages without it.
package recipeimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Recipe is the imported result (ingredients are parsed lines).
type Recipe struct {
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Servings    int               `json:"servings"`
	PrepMinutes int               `json:"prep_minutes"`
	CookMinutes int               `json:"cook_minutes"`
	Course      string            `json:"course"`
	Cuisine     string            `json:"cuisine"`
	Tags        string            `json:"tags"`
	SourceURL   string            `json:"source_url"`
	SourceName  string            `json:"source_name"`
	ImageURL    string            `json:"image_url"`
	Ingredients []Parsed          `json:"ingredients"`
	Steps       []string          `json:"steps"`
	Nutrition   map[string]string `json:"nutrition"`
}

// Fetch downloads a page and extracts the recipe.
func Fetch(ctx context.Context, rawURL, ua string) (*Recipe, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("enter a full http(s) URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "en,*")
	res, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("the site answered HTTP %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 6<<20))
	if err != nil {
		return nil, err
	}
	r, err := Parse(string(body), u.String())
	if err != nil {
		return nil, err
	}
	return r, nil
}

var scriptRe = regexp.MustCompile(`(?is)<script[^>]*type\s*=\s*["']?application/ld\+json["']?[^>]*>(.*?)</script>`)

// Parse extracts a recipe from HTML.
func Parse(page, pageURL string) (*Recipe, error) {
	for _, m := range scriptRe.FindAllStringSubmatch(page, -1) {
		raw := strings.TrimSpace(m[1])
		raw = strings.TrimPrefix(raw, "<![CDATA[")
		raw = strings.TrimSuffix(raw, "]]>")
		var v any
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			// some sites leave control characters in; try a cleaned copy
			cleaned := regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f]`).ReplaceAllString(raw, "")
			if err := json.Unmarshal([]byte(cleaned), &v); err != nil {
				continue
			}
		}
		if node := findRecipe(v); node != nil {
			r := fromNode(node)
			r.SourceURL = pageURL
			if r.SourceName == "" {
				r.SourceName = hostOf(pageURL)
			}
			if r.Title != "" {
				return r, nil
			}
		}
	}
	// Fallback: title + og:image; the user fills in the rest.
	r := &Recipe{SourceURL: pageURL, SourceName: hostOf(pageURL), Nutrition: map[string]string{}}
	if m := regexp.MustCompile(`(?is)<meta[^>]+property=["']og:title["'][^>]+content=["']([^"']+)`).FindStringSubmatch(page); m != nil {
		r.Title = html.UnescapeString(strings.TrimSpace(m[1]))
	} else if m := regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`).FindStringSubmatch(page); m != nil {
		r.Title = html.UnescapeString(strings.TrimSpace(m[1]))
	}
	if m := regexp.MustCompile(`(?is)<meta[^>]+property=["']og:image["'][^>]+content=["']([^"']+)`).FindStringSubmatch(page); m != nil {
		r.ImageURL = strings.TrimSpace(m[1])
	}
	if m := regexp.MustCompile(`(?is)<meta[^>]+name=["']description["'][^>]+content=["']([^"']+)`).FindStringSubmatch(page); m != nil {
		r.Description = html.UnescapeString(strings.TrimSpace(m[1]))
	}
	if r.Title == "" {
		return nil, errors.New("no recipe data found on that page")
	}
	return r, nil
}

func hostOf(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(p.Host, "www.")
}

func isRecipeType(t any) bool {
	switch x := t.(type) {
	case string:
		return strings.EqualFold(x, "Recipe")
	case []any:
		for _, e := range x {
			if s, ok := e.(string); ok && strings.EqualFold(s, "Recipe") {
				return true
			}
		}
	}
	return false
}

func findRecipe(v any) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		if isRecipeType(x["@type"]) {
			return x
		}
		for _, k := range []string{"@graph", "mainEntity", "mainEntityOfPage", "itemListElement"} {
			if sub, ok := x[k]; ok {
				if r := findRecipe(sub); r != nil {
					return r
				}
			}
		}
	case []any:
		for _, e := range x {
			if r := findRecipe(e); r != nil {
				return r
			}
		}
	}
	return nil
}

func str(v any) string {
	switch x := v.(type) {
	case string:
		return clean(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case []any:
		if len(x) > 0 {
			return str(x[0])
		}
	case map[string]any:
		for _, k := range []string{"name", "text", "url", "@id"} {
			if s, ok := x[k]; ok {
				return str(s)
			}
		}
	}
	return ""
}

func strs(v any) []string {
	var out []string
	switch x := v.(type) {
	case string:
		for _, p := range strings.Split(x, ",") {
			if p = clean(p); p != "" {
				out = append(out, p)
			}
		}
	case []any:
		for _, e := range x {
			if s := str(e); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

var tagRe = regexp.MustCompile(`<[^>]+>`)

func clean(s string) string {
	s = html.UnescapeString(tagRe.ReplaceAllString(s, " "))
	return strings.Join(strings.Fields(s), " ")
}

func steps(v any) []string {
	var out []string
	switch x := v.(type) {
	case string:
		for _, l := range strings.Split(x, "\n") {
			if l = clean(l); l != "" {
				out = append(out, l)
			}
		}
	case []any:
		for _, e := range x {
			switch n := e.(type) {
			case string:
				if s := clean(n); s != "" {
					out = append(out, s)
				}
			case map[string]any:
				if items, ok := n["itemListElement"]; ok { // HowToSection
					out = append(out, steps(items)...)
				} else if s := str(n["text"]); s != "" {
					out = append(out, s)
				} else if s := str(n["name"]); s != "" {
					out = append(out, s)
				}
			}
		}
	case map[string]any:
		if items, ok := x["itemListElement"]; ok {
			return steps(items)
		}
		if s := str(x["text"]); s != "" {
			out = append(out, s)
		}
	}
	return out
}

var durRe = regexp.MustCompile(`(?i)P(?:(\d+)D)?T?(?:(\d+)H)?(?:(\d+)M)?`)

func minutes(v any) int {
	s := str(v)
	if s == "" {
		return 0
	}
	if m := durRe.FindStringSubmatch(s); m != nil && strings.HasPrefix(strings.ToUpper(s), "P") {
		d, _ := strconv.Atoi(m[1])
		h, _ := strconv.Atoi(m[2])
		mi, _ := strconv.Atoi(m[3])
		return d*1440 + h*60 + mi
	}
	n, _ := strconv.Atoi(regexp.MustCompile(`\d+`).FindString(s))
	return n
}

func yield(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case string:
		if m := regexp.MustCompile(`\d+`).FindString(x); m != "" {
			n, _ := strconv.Atoi(m)
			return n
		}
	case []any:
		for _, e := range x {
			if n := yield(e); n > 0 {
				return n
			}
		}
	}
	return 0
}

func image(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		if len(x) > 0 {
			return image(x[0])
		}
	case map[string]any:
		if s, ok := x["url"].(string); ok {
			return s
		}
		if s, ok := x["contentUrl"].(string); ok {
			return s
		}
	}
	return ""
}

func fromNode(n map[string]any) *Recipe {
	r := &Recipe{Nutrition: map[string]string{}}
	r.Title = str(n["name"])
	r.Description = str(n["description"])
	r.Servings = yield(n["recipeYield"])
	r.PrepMinutes = minutes(n["prepTime"])
	r.CookMinutes = minutes(n["cookTime"])
	if r.PrepMinutes == 0 && r.CookMinutes == 0 {
		r.CookMinutes = minutes(n["totalTime"])
	}
	r.Cuisine = str(n["recipeCuisine"])
	r.ImageURL = image(n["image"])
	if a, ok := n["author"]; ok {
		r.SourceName = str(a)
	}
	cats := strs(n["recipeCategory"])
	r.Course = course(cats)
	tags := append([]string{}, strs(n["keywords"])...)
	for _, c := range cats {
		if strings.ToLower(c) != r.Course {
			tags = append(tags, c)
		}
	}
	seen := map[string]bool{}
	var uniq []string
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !seen[t] && len(uniq) < 12 {
			seen[t] = true
			uniq = append(uniq, t)
		}
	}
	r.Tags = strings.Join(uniq, ", ")
	for _, line := range strs(n["recipeIngredient"]) {
		r.Ingredients = append(r.Ingredients, ParseLine(line))
	}
	if len(r.Ingredients) == 0 {
		for _, line := range strs(n["ingredients"]) {
			r.Ingredients = append(r.Ingredients, ParseLine(line))
		}
	}
	r.Steps = steps(n["recipeInstructions"])
	if nut, ok := n["nutrition"].(map[string]any); ok {
		for k, v := range nut {
			if strings.HasPrefix(k, "@") {
				continue
			}
			if s := str(v); s != "" {
				r.Nutrition[strings.TrimSuffix(k, "Content")] = s
			}
		}
	}
	return r
}

func course(cats []string) string {
	for _, c := range cats {
		l := strings.ToLower(c)
		switch {
		case strings.Contains(l, "breakfast"), strings.Contains(l, "brunch"):
			return "breakfast"
		case strings.Contains(l, "lunch"):
			return "lunch"
		case strings.Contains(l, "dessert"), strings.Contains(l, "cake"), strings.Contains(l, "sweet"), strings.Contains(l, "baking"):
			return "dessert"
		case strings.Contains(l, "snack"), strings.Contains(l, "appetizer"), strings.Contains(l, "starter"):
			return "snack"
		case strings.Contains(l, "side"), strings.Contains(l, "salad"):
			return "side"
		case strings.Contains(l, "drink"), strings.Contains(l, "cocktail"), strings.Contains(l, "beverage"), strings.Contains(l, "smoothie"):
			return "drink"
		case strings.Contains(l, "sauce"), strings.Contains(l, "dip"), strings.Contains(l, "dressing"):
			return "sauce"
		case strings.Contains(l, "main"), strings.Contains(l, "dinner"), strings.Contains(l, "entree"), strings.Contains(l, "entrée"):
			return "dinner"
		}
	}
	return "dinner"
}
