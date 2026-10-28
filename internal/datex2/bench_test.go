package datex2

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkParseSituations measures the streaming parse of the real incident
// fixture (docs/feeds.md §10: 7.17 MB per payload, decoded once per minute).
func BenchmarkParseSituations(b *testing.B) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "feeds", "situations", "snap-1.xml"))
	if err != nil {
		b.Fatalf("read fixture: %v", err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ParseSituations(bytes.NewReader(data)); err != nil {
			b.Fatalf("ParseSituations: %v", err)
		}
	}
}
