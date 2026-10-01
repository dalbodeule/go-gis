package scripting

import (
	"context"
	"runtime"
	"testing"

	"gogis/internal/commands"
	"gogis/internal/core"
)

var benchmarkLabelValue string
var benchmarkLabelMatch bool

func benchmarkLabelProperties10K() []map[string]any {
	properties := make([]map[string]any, 10_000)
	for index := range properties {
		properties[index] = map[string]any{
			"NAME": "한강로", "LANES": index % 8, "CLASS": "primary",
		}
	}
	return properties
}

func BenchmarkLabelProgramEvaluateText10KFeatures(b *testing.B) {
	program, err := CompileLabelProgram(`return string.format("%s · %d차선", feature.NAME, feature.LANES)`)
	if err != nil {
		b.Fatal(err)
	}
	defer program.Close()
	properties := benchmarkLabelProperties10K()
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		for _, feature := range properties {
			benchmarkLabelValue, err = program.EvaluateText(context.Background(), feature)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*len(properties)), "ns/feature")
}

func BenchmarkLabelProgramEvaluateRule10KFeatures(b *testing.B) {
	program, err := CompileLabelProgram(`return feature.CLASS == "primary" and feature.LANES >= 4`)
	if err != nil {
		b.Fatal(err)
	}
	defer program.Close()
	properties := benchmarkLabelProperties10K()
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		for _, feature := range properties {
			benchmarkLabelMatch, err = program.EvaluateRule(context.Background(), feature)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*len(properties)), "ns/feature")
}

func BenchmarkLabelComposerProgramEvaluate10KFeatures(b *testing.B) {
	program, err := CompileLabelComposerProgram(
		`return string.format("%s · %d차선", feature.NAME, feature.LANES)`,
		`return feature.CLASS == "primary" and feature.LANES >= 4`,
	)
	if err != nil {
		b.Fatal(err)
	}
	defer program.Close()
	properties := benchmarkLabelProperties10K()
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		for _, feature := range properties {
			benchmarkLabelValue, benchmarkLabelMatch, err = program.Evaluate(context.Background(), feature)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*len(properties)), "ns/feature")
}

func BenchmarkLabelComposerProgramEvaluate1MFeatures(b *testing.B) {
	program, err := CompileLabelComposerProgram(
		`return string.format("%s · %d차선", feature.NAME, feature.LANES)`,
		`return feature.CLASS == "primary" and feature.LANES >= 4`,
	)
	if err != nil {
		b.Fatal(err)
	}
	defer program.Close()
	properties := benchmarkLabelProperties10K()
	const featureEvaluations = 1_000_000
	b.ReportAllocs()
	runtime.GC()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		for index := 0; index < featureEvaluations; index++ {
			benchmarkLabelValue, benchmarkLabelMatch, err = program.Evaluate(context.Background(), properties[index%len(properties)])
			if err != nil {
				b.Fatal(err)
			}
		}
	}
	b.StopTimer()
	runtime.GC()
	var heapAfter runtime.MemStats
	runtime.ReadMemStats(&heapAfter)
	b.ReportMetric(float64(heapAfter.HeapAlloc)/(1<<20), "heap-after-GC-MiB")
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*featureEvaluations), "ns/feature")
}

func BenchmarkSeparateLabelProgramsEvaluate10KFeatures(b *testing.B) {
	textProgram, err := CompileLabelProgram(`return string.format("%s · %d차선", feature.NAME, feature.LANES)`)
	if err != nil {
		b.Fatal(err)
	}
	defer textProgram.Close()
	ruleProgram, err := CompileLabelProgram(`return feature.CLASS == "primary" and feature.LANES >= 4`)
	if err != nil {
		b.Fatal(err)
	}
	defer ruleProgram.Close()
	properties := benchmarkLabelProperties10K()
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		for _, feature := range properties {
			benchmarkLabelMatch, err = ruleProgram.EvaluateRule(context.Background(), feature)
			if err != nil {
				b.Fatal(err)
			}
			benchmarkLabelValue, err = textProgram.EvaluateText(context.Background(), feature)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*len(properties)), "ns/feature")
}

func BenchmarkLuaFilterPredicate10KFeatures(b *testing.B) {
	program, err := CompileLabelComposerProgram("return nil", `return feature.CLASS == "primary" and feature.LANES >= 4`)
	if err != nil {
		b.Fatal(err)
	}
	defer program.Close()
	features := make([]core.Feature, 10_000)
	for index := range features {
		features[index] = core.Feature{
			ID: uint64(index + 1),
			Properties: map[string]any{
				"CLASS": "primary", "LANES": index % 8,
			},
		}
	}
	layer := core.Layer{Name: "roads", Features: features}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		result, err := commands.FilterLayerByPredicate(context.Background(), layer, func(ctx context.Context, feature core.Feature) (bool, error) {
			_, matched, evaluateErr := program.Evaluate(ctx, feature.Properties)
			return matched, evaluateErr
		})
		if err != nil {
			b.Fatal(err)
		}
		benchmarkFilterResult = result
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*len(features)), "ns/feature")
}

var benchmarkFilterResult core.Layer
