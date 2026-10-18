package store

import "testing"

func TestNormalizeName(t *testing.T) {
	for in, want := range map[string]string{"Tomatoes": "tomato", "Eggs": "egg", "large eggs": "large egg", "Canned tomatoes": "canned tomato", "Glass": "glass", "berries": "berry", "Rolled oats": "rolled oat", "peaches": "peach"} {
		if got := NormalizeName(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
	if stripDescriptors("large free-range eggs") != "eggs" || stripDescriptors("spring onion") != "spring onion" {
		t.Error("descriptor stripping")
	}
}
