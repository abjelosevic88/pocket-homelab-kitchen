package units

import "testing"

func TestConvertAndFormat(t *testing.T) {
	if v, ok := Convert(1.5, "kg", "g"); !ok || v != 1500 {
		t.Errorf("kg→g: %v %v", v, ok)
	}
	if v, ok := Convert(2, "tbsp", "ml"); !ok || v != 30 {
		t.Errorf("tbsp→ml: %v %v", v, ok)
	}
	if _, ok := Convert(1, "g", "ml"); ok {
		t.Error("g→ml must not convert")
	}
	if _, ok := Convert(1, "clove", "pcs"); ok {
		t.Error("clove→pcs must not convert")
	}
	if Normalize("Grams") != "g" || Normalize("Tablespoons") != "tbsp" || Normalize("x") != "pcs" {
		t.Error("normalize")
	}
	if Format(1.5, "kg") != "1.5 kg" || Format(2, "pcs") != "2" || Format(0, "pinch") != "pinch" || FormatZero(0, "g") != "0 g" || FormatZero(0, "pcs") != "0" {
		t.Error("format")
	}
	q, u := Pretty(1500, Mass, "g")
	if q != 1.5 || u != "kg" {
		t.Errorf("pretty: %v %v", q, u)
	}
}
