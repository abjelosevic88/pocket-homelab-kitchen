// Package units normalises ingredient units so shopping lists can merge
// "200 g flour" with "0.5 kg flour" and the pantry can be deducted sensibly.
package units

import (
	"fmt"
	"math"
	"strings"
)

// Family groups units that convert into each other.
type Family string

const (
	Mass   Family = "mass"   // base: g
	Volume Family = "volume" // base: ml
	Count  Family = "count"  // base: pcs (unit kept as written)
	None   Family = ""       // unknown free text, never merged across spellings
)

type def struct {
	canon  string
	family Family
	factor float64 // to base
}

var table = map[string]def{
	"g": {"g", Mass, 1}, "gram": {"g", Mass, 1}, "grams": {"g", Mass, 1}, "gr": {"g", Mass, 1},
	"kg": {"kg", Mass, 1000}, "kilogram": {"kg", Mass, 1000}, "kilograms": {"kg", Mass, 1000},
	"mg": {"mg", Mass, 0.001},
	"oz": {"oz", Mass, 28.3495}, "ounce": {"oz", Mass, 28.3495}, "ounces": {"oz", Mass, 28.3495},
	"lb": {"lb", Mass, 453.592}, "lbs": {"lb", Mass, 453.592}, "pound": {"lb", Mass, 453.592}, "pounds": {"lb", Mass, 453.592},
	"ml": {"ml", Volume, 1}, "milliliter": {"ml", Volume, 1}, "millilitre": {"ml", Volume, 1}, "milliliters": {"ml", Volume, 1}, "millilitres": {"ml", Volume, 1},
	"cl": {"cl", Volume, 10}, "dl": {"dl", Volume, 100},
	"l": {"l", Volume, 1000}, "liter": {"l", Volume, 1000}, "litre": {"l", Volume, 1000}, "liters": {"l", Volume, 1000}, "litres": {"l", Volume, 1000},
	"tsp": {"tsp", Volume, 5}, "teaspoon": {"tsp", Volume, 5}, "teaspoons": {"tsp", Volume, 5},
	"tbsp": {"tbsp", Volume, 15}, "tablespoon": {"tbsp", Volume, 15}, "tablespoons": {"tbsp", Volume, 15}, "tbs": {"tbsp", Volume, 15},
	"cup": {"cup", Volume, 240}, "cups": {"cup", Volume, 240},
	"fl oz": {"fl oz", Volume, 29.5735}, "floz": {"fl oz", Volume, 29.5735},
	"pint": {"pint", Volume, 473.176}, "pints": {"pint", Volume, 473.176}, "quart": {"quart", Volume, 946.353}, "quarts": {"quart", Volume, 946.353},
	"pcs": {"pcs", Count, 1}, "pc": {"pcs", Count, 1}, "piece": {"pcs", Count, 1}, "pieces": {"pcs", Count, 1}, "x": {"pcs", Count, 1}, "": {"", Count, 1},
	"clove": {"clove", Count, 1}, "cloves": {"clove", Count, 1},
	"slice": {"slice", Count, 1}, "slices": {"slice", Count, 1},
	"can": {"can", Count, 1}, "cans": {"can", Count, 1}, "tin": {"can", Count, 1}, "tins": {"can", Count, 1},
	"pack": {"pack", Count, 1}, "packs": {"pack", Count, 1}, "package": {"pack", Count, 1}, "packet": {"pack", Count, 1},
	"bunch": {"bunch", Count, 1}, "bunches": {"bunch", Count, 1},
	"pinch": {"pinch", Count, 1}, "pinches": {"pinch", Count, 1}, "dash": {"dash", Count, 1},
	"stick": {"stick", Count, 1}, "sticks": {"stick", Count, 1}, "sprig": {"sprig", Count, 1}, "sprigs": {"sprig", Count, 1},
	"handful": {"handful", Count, 1}, "leaf": {"leaf", Count, 1}, "leaves": {"leaf", Count, 1},
}

// Normalize returns the canonical spelling of a unit ("grams" → "g").
// Unknown units come back trimmed and lower-cased.
func Normalize(u string) string {
	u = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(u), ".")))
	if d, ok := table[u]; ok {
		return d.canon
	}
	return u
}

// FamilyOf returns the family of a unit.
func FamilyOf(u string) Family {
	if d, ok := table[Normalize(u)]; ok {
		return d.family
	}
	return None
}

// ToBase converts a quantity into the family base (g, ml or count).
func ToBase(qty float64, u string) (float64, Family) {
	d, ok := table[Normalize(u)]
	if !ok {
		return qty, None
	}
	return qty * d.factor, d.family
}

// Convert converts between two units of the same family. ok=false otherwise.
func Convert(qty float64, from, to string) (float64, bool) {
	a, fa := ToBase(qty, from)
	df, ok := table[Normalize(to)]
	if !ok || fa == None || df.family != fa || (fa == Count && Normalize(from) != Normalize(to)) {
		return qty, false
	}
	return a / df.factor, true
}

// Pretty renders a base quantity in a human unit: 1500 g → "1.5 kg", 250 ml → "250 ml".
func Pretty(base float64, f Family, keepUnit string) (float64, string) {
	switch f {
	case Mass:
		if base >= 1000 {
			return round(base/1000, 2), "kg"
		}
		return round(base, 1), "g"
	case Volume:
		if base >= 1000 {
			return round(base/1000, 2), "l"
		}
		return round(base, 1), "ml"
	}
	return round(base, 2), Normalize(keepUnit)
}

func round(v float64, places int) float64 {
	p := math.Pow10(places)
	return math.Round(v*p) / p
}

// Format renders "1.5 kg", "2" (pieces), "pinch" (a unit with no quantity) or "".
func Format(qty float64, unit string) string {
	if qty == 0 {
		if unit != "" {
			return unit
		}
		return ""
	}
	return FormatZero(qty, unit)
}

// FormatZero is Format but renders a zero quantity as "0" / "0 g" (for "… left" messages).
func FormatZero(qty float64, unit string) string {
	if qty == 0 {
		if unit == "" || unit == "pcs" {
			return "0"
		}
		return "0 " + unit
	}
	q := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", qty), "0"), ".")
	if unit == "" || unit == "pcs" {
		return q
	}
	return q + " " + unit
}
