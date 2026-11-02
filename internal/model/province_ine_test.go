package model

import "testing"

// TestProvinceFromINECoversAll52Codes pins that the INE table holds exactly
// the 52 Spanish provinces (50 provinces plus Ceuta and Melilla): every
// two-digit code from 01 to 52 resolves and there are no extra entries.
func TestProvinceFromINECoversAll52Codes(t *testing.T) {
	if len(provinceINE) != 52 {
		t.Fatalf("the INE table holds %d entries, want 52 (50 provinces + Ceuta + Melilla)", len(provinceINE))
	}
	for code := 1; code <= 52; code++ {
		twoDigit := fmtCode(code)
		if name, ok := ProvinceFromINE(twoDigit); !ok || name == "" {
			t.Errorf("ProvinceFromINE(%q) = %q, %v; want a province name", twoDigit, name, ok)
		}
	}
}

// TestProvinceFromINEKnownCodes pins representative lookups, including the
// single-digit spelling the radar feed actually publishes
// (provinceINEIdentifier "4" for Almería, docs/feeds.md §6) and the
// bilingual forms the incident feed publishes (docs/feeds.md §4.6), so
// --province behaves the same across commands.
func TestProvinceFromINEKnownCodes(t *testing.T) {
	cases := []struct {
		code string
		want string
	}{
		{"50", "Zaragoza"},
		{"4", "Almería"},
		{"04", "Almería"},
		{"15", "A Coruña"},
		{"26", "La Rioja"},
		{"03", "Alacant/Alicante"},
		{"12", "Castelló/Castellón"},
		{"46", "València/Valencia"},
		{"51", "Ceuta"},
		{"52", "Melilla"},
	}
	for _, tc := range cases {
		got, ok := ProvinceFromINE(tc.code)
		if !ok || got != tc.want {
			t.Errorf("ProvinceFromINE(%q) = %q, %v; want %q, true", tc.code, got, ok, tc.want)
		}
	}
}

// TestProvinceFromINEUnknownCodeNeverGuesses pins that an unknown code
// returns false so the caller serialises province as null — never a guess.
func TestProvinceFromINEUnknownCodeNeverGuesses(t *testing.T) {
	for _, code := range []string{"", "99", "00", "53", "123", "ab", "5a"} {
		if name, ok := ProvinceFromINE(code); ok {
			t.Errorf("ProvinceFromINE(%q) = %q, true; want false", code, name)
		}
	}
}

// TestProvinceFromINENamesMatchProvinceFilter pins the whole point of the
// table: the name it returns matches through model.ProvinceEqual, so
// --province zaragoza matches the radar whose INE code is 50 and
// --province valencia matches the bilingual pair of code 46.
func TestProvinceFromINENamesMatchProvinceFilter(t *testing.T) {
	zaragoza, ok := ProvinceFromINE("50")
	if !ok || !ProvinceEqual(zaragoza, "zaragoza") {
		t.Errorf("code 50 name %q does not match --province zaragoza", zaragoza)
	}
	valencia, ok := ProvinceFromINE("46")
	if !ok || !ProvinceEqual(valencia, "valencia") || !ProvinceEqual(valencia, "València") {
		t.Errorf("code 46 name %q does not match either half of the bilingual pair", valencia)
	}
}

// fmtCode renders a code as the two-digit zero-padded string of n.
func fmtCode(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}
