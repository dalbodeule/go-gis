package presentation

import (
	"fmt"
	"testing"

	"gogis/internal/core"
)

var attributeTableBenchmarkSink AttributeTableModel

func BenchmarkAttributeTableCompleteSchema200Rows(b *testing.B) {
	layer := benchmarkAttributeLayer(200, false)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		attributeTableBenchmarkSink = AttributeTable(layer)
	}
}

func BenchmarkAttributeTableOwnedCompleteSchema200Rows(b *testing.B) {
	layer := benchmarkAttributeLayer(200, false)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		attributeTableBenchmarkSink = AttributeTableOwned(layer)
	}
}

func BenchmarkAttributeTableUndeclaredField200Rows(b *testing.B) {
	layer := benchmarkAttributeLayer(200, true)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		attributeTableBenchmarkSink = AttributeTable(layer)
	}
}

func benchmarkAttributeLayer(featureCount int, undeclared bool) core.Layer {
	features := make([]core.Feature, featureCount)
	for index := range features {
		properties := map[string]any{
			"name":  fmt.Sprintf("road-%d", index),
			"value": index,
		}
		if undeclared {
			properties["extra"] = index%2 == 0
		}
		features[index] = core.Feature{ID: uint64(index + 1), Properties: properties}
	}
	return core.Layer{
		Fields:   []core.Field{{Name: "name", Type: core.FieldTypeText}, {Name: "value", Type: core.FieldTypeNumber}},
		Features: features,
	}
}
