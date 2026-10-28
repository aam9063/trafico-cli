package datex2

import (
	"bytes"
	"strings"
	"testing"
)

// A payload cut off mid-element must return an error, not a panic and not a
// partial Publication.
func TestParseSituations_TruncatedXMLErrors(t *testing.T) {
	data := fixture(t, "snap-1.xml")
	cuts := []struct {
		name string
		n    int
	}{
		{"missing last 200 bytes", len(data) - 200},     // inside the last situation element
		{"missing root closing bracket", len(data) - 2}, // `</d2:payload>` loses its `>`
		{"half the document", len(data) / 2},
	}
	for _, cut := range cuts {
		t.Run(cut.name, func(t *testing.T) {
			pub, err := ParseSituations(bytes.NewReader(data[:cut.n]))
			if err == nil {
				t.Fatalf("ParseSituations succeeded on truncated input, got %+v", pub)
			}
		})
	}
}

func TestParseSituations_EmptyBodyErrors(t *testing.T) {
	for _, body := range []string{"", "   \n\t"} {
		pub, err := ParseSituations(strings.NewReader(body))
		if err == nil {
			t.Fatalf("ParseSituations(%q) succeeded, got %+v", body, pub)
		}
	}
}

// A well-formed payload without publicationTime is unusable: publicationTime
// is the only valid freshness source (docs/feeds.md §2).
func TestParseSituations_MissingPublicationTimeErrors(t *testing.T) {
	const body = `<?xml version="1.0" encoding="UTF-8"?>
<d2:payload xsi:type="sit:SituationPublication" xmlns:d2="http://levelC/schema/3/d2Payload" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
</d2:payload>`
	pub, err := ParseSituations(strings.NewReader(body))
	if err == nil {
		t.Fatalf("ParseSituations succeeded without publicationTime, got %+v", pub)
	}
}

// Malformed XML inside an element must surface as an error.
func TestParseSituations_MalformedXMLErrors(t *testing.T) {
	const body = `<?xml version="1.0" encoding="UTF-8"?>
<d2:payload xmlns:d2="http://levelC/schema/3/d2Payload">
  <com:publicationTime>not-a-timestamp</com:publicationTime>
</d2:payload>`
	pub, err := ParseSituations(strings.NewReader(body))
	if err == nil {
		t.Fatalf("ParseSituations succeeded on invalid timestamp, got %+v", pub)
	}
}
