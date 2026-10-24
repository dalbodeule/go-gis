package commands

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"gogis/internal/core"
)

func BenchmarkImportCSVPoints100k(b *testing.B) {
	benchmarkImportCSVPoints(b, 100000)
}

func BenchmarkImportCSVPoints1M(b *testing.B) {
	benchmarkImportCSVPoints(b, 1000000)
}

func benchmarkImportCSVPoints(b *testing.B, rows int) {
	var csv strings.Builder
	csv.WriteString("id,x,y,name\n")
	for i := 1; i <= rows; i++ {
		csv.WriteString(strconv.Itoa(i))
		csv.WriteByte(',')
		csv.WriteString(strconv.Itoa(i))
		csv.WriteString(",")
		csv.WriteString(strconv.Itoa(i + 1))
		csv.WriteString(",시설-")
		csv.WriteString(strconv.Itoa(i))
		csv.WriteByte('\n')
	}
	data := csv.String()
	b.ReportMetric(float64(len(data)), "bytes/input")
	b.ReportMetric(float64(rows), "rows/input")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := ImportCSVPoints(context.Background(), strings.NewReader(data), CSVOptions{
			Delimiter: ',', HasHeader: true, XField: "x", YField: "y",
			CRS: core.CRS{AuthorityCode: "EPSG:4326"}, LayerName: "bench",
		})
		if err != nil || result.Accepted != rows {
			b.Fatalf("import failed: accepted=%d err=%v", result.Accepted, err)
		}
	}
}
