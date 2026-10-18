package recipeimport

import (
	"regexp"
	"strconv"
	"strings"
)

// Parsed is one ingredient line split into its parts.
type Parsed struct {
	Qty  float64
	Unit string
	Name string
	Note string
}

var fractions = map[string]float64{"½": 0.5, "⅓": 1.0 / 3, "⅔": 2.0 / 3, "¼": 0.25, "¾": 0.75, "⅕": 0.2, "⅖": 0.4, "⅗": 0.6, "⅘": 0.8, "⅙": 1.0 / 6, "⅚": 5.0 / 6, "⅛": 0.125, "⅜": 0.375, "⅝": 0.625, "⅞": 0.875}

var unitWords = []string{"kg", "g", "gr", "grams", "gram", "mg", "oz", "ounces", "ounce", "lbs", "lb", "pounds", "pound", "ml", "cl", "dl", "l", "liters", "litres", "liter", "litre", "tsp", "teaspoons", "teaspoon", "tbsp", "tbs", "tablespoons", "tablespoon", "cups", "cup", "pcs", "pc", "pieces", "piece", "cloves", "clove", "slices", "slice", "cans", "can", "tins", "tin", "packs", "pack", "packages", "package", "packet", "bunches", "bunch", "pinches", "pinch", "dash", "sticks", "stick", "sprigs", "sprig", "handful", "leaves", "leaf", "fl oz", "pint", "pints", "quart", "quarts", "kom", "kašika", "kašike", "kašičica", "kašičice", "šolja", "šolje"}

var numRe = regexp.MustCompile(`^(\d+)/(\d+)(?:\s|$)|^(\d+(?:[.,]\d+)?)(?:\s*[-–]\s*\d+(?:[.,]\d+)?)?(?:\s+(\d+)/(\d+))?`)

// ParseLine splits "1 1/2 cups flour, sifted" into qty 1.5, unit "cup", name "flour", note "sifted".
func ParseLine(line string) Parsed {
	s := strings.TrimSpace(line)
	s = strings.TrimLeft(s, "-•* ")
	for k, v := range fractions {
		if strings.Contains(s, k) {
			s = strings.Replace(s, k, " "+strconv.FormatFloat(v, 'f', 3, 64), 1)
		}
	}
	s = strings.Join(strings.Fields(s), " ")
	p := Parsed{}
	if m := numRe.FindStringSubmatch(s); m != nil && m[0] != "" {
		if m[1] != "" { // bare fraction "1/2"
			a, _ := strconv.ParseFloat(m[1], 64)
			b, _ := strconv.ParseFloat(m[2], 64)
			if b > 0 {
				p.Qty = a / b
			}
		} else {
			p.Qty, _ = strconv.ParseFloat(strings.ReplaceAll(m[3], ",", "."), 64)
			if m[4] != "" {
				a, _ := strconv.ParseFloat(m[4], 64)
				b, _ := strconv.ParseFloat(m[5], 64)
				if b > 0 {
					p.Qty += a / b
				}
			}
		}
		s = strings.TrimSpace(s[len(m[0]):])
	}
	// "(200 g)" style parentheses after a count: keep as note
	low := strings.ToLower(s)
	for _, u := range unitWords {
		if strings.HasPrefix(low, u+" ") || strings.HasPrefix(low, u+".") || low == u {
			rest := strings.TrimSpace(s[len(u):])
			rest = strings.TrimPrefix(rest, ".")
			rest = strings.TrimSpace(strings.TrimPrefix(rest, "of "))
			p.Unit = u
			s = rest
			break
		}
	}
	// a quantity glued to the unit: "200g"
	if p.Unit == "" && p.Qty == 0 {
		if m := regexp.MustCompile(`^(\d+(?:[.,]\d+)?)([a-zA-Z]+)\b\s*(.*)$`).FindStringSubmatch(s); m != nil {
			for _, u := range unitWords {
				if strings.EqualFold(m[2], u) {
					p.Qty, _ = strconv.ParseFloat(strings.ReplaceAll(m[1], ",", "."), 64)
					p.Unit = u
					s = strings.TrimSpace(strings.TrimPrefix(m[3], "of "))
					break
				}
			}
		}
	}
	// note after a comma or in parentheses
	if i := strings.Index(s, ","); i > 0 {
		p.Note = strings.TrimSpace(s[i+1:])
		s = strings.TrimSpace(s[:i])
	}
	if i := strings.Index(s, "("); i > 0 {
		if j := strings.Index(s, ")"); j > i {
			n := strings.TrimSpace(s[i+1 : j])
			if p.Note == "" {
				p.Note = n
			} else {
				p.Note = n + ", " + p.Note
			}
			s = strings.TrimSpace(s[:i] + s[j+1:])
		}
	}
	p.Name = strings.TrimSpace(s)
	if p.Name == "" {
		p.Name = strings.TrimSpace(line)
		p.Qty, p.Unit, p.Note = 0, "", ""
	}
	return p
}
