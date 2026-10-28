package model

import "testing"

// TestNormalizeProvince covers the bilingual and case variants measured in the
// feed (docs/feeds.md §4.6): the value is lowercased, accents are folded, and
// both halves of a slash-separated bilingual name are preserved.
func TestNormalizeProvince(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "already lower case", input: "lugo", want: "lugo"},
		{name: "upper case", input: "LUGO", want: "lugo"},
		{name: "mixed case", input: "Lugo", want: "lugo"},
		{name: "bilingual pair kept whole", input: "Alacant/Alicante", want: "alacant/alicante"},
		{name: "accents folded", input: "A Coruña", want: "a coruna"},
		{name: "trimmed", input: "  València/Valencia  ", want: "valencia/valencia"},
		{name: "empty", input: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeProvince(tt.input); got != tt.want {
				t.Errorf("NormalizeProvince(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestProvinceEqual proves the --province filter matches either half of a
// bilingual name and is insensitive to case and accents.
func TestProvinceEqual(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{name: "case insensitive", a: "Lugo", b: "LUGO", want: true},
		{name: "bilingual second half", a: "Alacant/Alicante", b: "alicante", want: true},
		{name: "bilingual first half", a: "Alacant/Alicante", b: "alacant", want: true},
		{name: "accents folded", a: "Castelló/Castellón", b: "castello", want: true},
		{name: "different provinces", a: "Lugo", b: "Ourense", want: false},
		{name: "both empty", a: "", b: "", want: true},
		{name: "empty vs province", a: "", b: "lugo", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ProvinceEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("ProvinceEqual(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
