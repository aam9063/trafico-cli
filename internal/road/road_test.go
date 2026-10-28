package road

import "testing"

// TestNormalize covers every real road-name format measured in the feed
// (docs/feeds.md §4.7) plus the whitespace and case variants that motivated
// the normalisation rule. The normaliser must uppercase, trim and collapse
// whitespace, insert the single hyphen of a letter-prefixed numbered road,
// and — above all — never invent a hyphen where the source has none.
func TestNormalize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "already canonical", input: "A-7", want: "A-7"},
		{name: "motorway prefix", input: "AP-7", want: "AP-7"},
		{name: "national with suffix letter", input: "N-332a", want: "N-332A"},
		{name: "regional", input: "CV-80", want: "CV-80"},
		{name: "ramal suffix", input: "A-356R1", want: "A-356R1"},
		{name: "decimal kilometre suffix", input: "CG-1.5", want: "CG-1.5"},
		{name: "provincial lowercase", input: "Ma-10", want: "MA-10"},
		{name: "long provincial prefix", input: "LU-P-1611", want: "LU-P-1611"},
		{name: "urban prefix of several letters", input: "CU-V-5006", want: "CU-V-5006"},
		{name: "extremadura prefix", input: "EX-A1", want: "EX-A1"},
		{name: "diputacion prefix", input: "DSA-291", want: "DSA-291"},
		{name: "text suffix kept as written", input: "GC-1 AM", want: "GC-1 AM"},
		{name: "free text prefix kept", input: "CHG-1", want: "CHG-1"},
		{name: "free text name, no invented hyphen", input: "CV privado Iberdrola", want: "CV PRIVADO IBERDROLA"},
		{name: "urban street, trailing space, accents kept", input: "Avda. Andalucía ", want: "AVDA. ANDALUCÍA"},
		{name: "empty string", input: "", want: ""},
		{name: "no separator between prefix and number", input: "a7", want: "A-7"},
		{name: "space instead of hyphen", input: "A 7", want: "A-7"},
		{name: "lowercase with hyphen", input: "a-7", want: "A-7"},
		{name: "leading and doubled whitespace", input: "  a7  ", want: "A-7"},
		{name: "internal doubled whitespace", input: "GC-1  AM", want: "GC-1 AM"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Normalize(tt.input); got != tt.want {
				t.Errorf("Normalize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestEqual proves the comparison used by the --road filter accepts the case
// and separator variants of the same road (docs/feeds.md §4.7).
func TestEqual(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{name: "space vs hyphen", a: "A 7", b: "a-7", want: true},
		{name: "case only", a: "N-332a", b: "N-332A", want: true},
		{name: "identical", a: "AP-7", b: "AP-7", want: true},
		{name: "different roads", a: "A-7", b: "AP-7", want: false},
		{name: "both empty", a: "", b: " ", want: true},
		{name: "empty vs road", a: "", b: "A-7", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Equal(tt.a, tt.b); got != tt.want {
				t.Errorf("Equal(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
