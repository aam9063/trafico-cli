package model

import "strings"

// accentFold maps the accented characters that appear in Spanish province
// names to their unaccented form. Provinces are folded only for comparison
// and filtering; the incident keeps the source text as published (P-04).
var accentFold = strings.NewReplacer(
	"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
	"à", "a", "è", "e", "ì", "i", "ò", "o", "ù", "u",
	"â", "a", "ê", "e", "î", "i", "ô", "o", "û", "u",
	"ç", "c", "ý", "y",
)

// NormalizeProvince folds a province name for comparison (docs/feeds.md
// §4.6): trimmed, lowercased, accents stripped. A slash-separated bilingual
// name (Alacant/Alicante, Castelló/Castellón, València/Valencia) keeps both
// halves, so either one stays matchable.
func NormalizeProvince(name string) string {
	return accentFold.Replace(strings.ToLower(strings.TrimSpace(name)))
}

// ProvinceEqual reports whether two province names name the same province,
// matching either half of a bilingual pair and ignoring case and accents.
// It is the comparison the --province filter uses.
func ProvinceEqual(a, b string) bool {
	na, nb := NormalizeProvince(a), NormalizeProvince(b)
	if na == nb {
		return true
	}
	for _, ha := range strings.Split(na, "/") {
		for _, hb := range strings.Split(nb, "/") {
			if ha != "" && ha == hb {
				return true
			}
		}
	}
	return false
}
