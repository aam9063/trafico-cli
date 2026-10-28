package road

import (
	"regexp"
	"strings"
)

// numberedRoad matches a letter prefix (1-3 letters, the widest real prefix
// measured is DSA) optionally separated from the number by a space or a dot.
// The separator class deliberately excludes "-": a name that already carries
// a hyphen keeps its structure untouched (A-356R1, CG-1.5, LU-P-1611), and a
// free-text name never gains one (Avda. Andalucía, CV privado Iberdrola,
// CHG-1) because no digit follows the prefix. Prefixes longer than 3 letters
// (AVDA, CALLE) are street names, not road codes, and are left alone.
var numberedRoad = regexp.MustCompile(`^([A-Z]{1,3})[ .]?(\d.*)$`)

// Normalize returns the canonical form of a road name (docs/feeds.md §4.7):
//
//   - trimmed, internal whitespace collapsed, uppercased (accents preserved);
//   - a letter prefix followed by a number gets exactly one hyphen inserted,
//     so a7, "A 7", "a-7" and "A-7" all normalize to "A-7";
//   - names that already carry a hyphen keep their structure and suffixes
//     (N-332a -> N-332A, A-356R1, CG-1.5, LU-P-1611);
//   - names that are not a letter-prefixed number survive as written apart
//     from case and whitespace: no hyphen is ever invented where the source
//     has none (GC-1 AM, CHG-1, CV privado Iberdrola, Avda. Andalucía).
//
// The empty string normalizes to the empty string.
func Normalize(name string) string {
	s := strings.ToUpper(strings.Join(strings.Fields(name), " "))
	if m := numberedRoad.FindStringSubmatch(s); m != nil {
		return m[1] + "-" + m[2]
	}
	return s
}

// Equal reports whether two road names are the same road once normalized. It
// is the comparison the --road filter uses, so "A 7" matches "a-7" and a user
// typing "n-332A" or "N-332a" matches the same feed name.
func Equal(a, b string) bool {
	return Normalize(a) == Normalize(b)
}
