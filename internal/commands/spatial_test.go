package commands

import (
	"context"
	"errors"
	"testing"

	"gogis/internal/core"
)

type fakeSpatialOperator struct {
	operation string
	result    core.Layer
	err       error
}

func (f *fakeSpatialOperator) Intersect(context.Context, core.Layer, core.Layer) (core.Layer, error) {
	f.operation = "intersect"
	return f.result, f.err
}
func (f *fakeSpatialOperator) Union(context.Context, core.Layer, core.Layer) (core.Layer, error) {
	f.operation = "union"
	return f.result, f.err
}
func (f *fakeSpatialOperator) Difference(context.Context, core.Layer, core.Layer) (core.Layer, error) {
	f.operation = "difference"
	return f.result, f.err
}
func (f *fakeSpatialOperator) Buffer(context.Context, core.Layer, float64) (core.Layer, error) {
	f.operation = "buffer"
	return f.result, f.err
}

func TestApplySpatialOperationDispatches(t *testing.T) {
	for _, operation := range []string{"intersect", "union", "difference", "buffer"} {
		operator := &fakeSpatialOperator{}
		if _, err := ApplySpatialOperation(context.Background(), operator, operation, core.Layer{}, core.Layer{}, 2); err != nil {
			t.Fatalf("%s: %v", operation, err)
		}
		if operator.operation != operation {
			t.Fatalf("dispatched %q as %q", operation, operator.operation)
		}
	}
}

func TestApplySpatialOperationValidatesContextAndOperation(t *testing.T) {
	operator := &fakeSpatialOperator{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ApplySpatialOperation(ctx, operator, "buffer", core.Layer{}, core.Layer{}, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
	if _, err := ApplySpatialOperation(context.Background(), operator, "erase", core.Layer{}, core.Layer{}, 1); !errors.Is(err, ErrUnknownSpatialOperation) {
		t.Fatalf("unknown operation error = %v", err)
	}
}

func TestApplySpatialOperationRejectsCRSMismatch(t *testing.T) {
	operator := &fakeSpatialOperator{}
	_, err := ApplySpatialOperation(context.Background(), operator, "intersect",
		core.Layer{CRS: core.CRS{AuthorityCode: "EPSG:4326"}},
		core.Layer{CRS: core.CRS{AuthorityCode: "EPSG:5179"}}, 0)
	if !errors.Is(err, ErrSpatialCRSMismatch) {
		t.Fatalf("CRS error = %v", err)
	}
	if operator.operation != "" {
		t.Fatal("operator was called despite CRS mismatch")
	}
}

func TestProjectServiceAppliesSpatialResultAtomically(t *testing.T) {
	service := NewProjectService("demo", core.CRS{AuthorityCode: "EPSG:4326"})
	if err := service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"left", "right"} {
		if err := service.AddLayer(core.Layer{Name: name, CRS: core.CRS{AuthorityCode: "EPSG:4326"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.Commit(); err != nil {
		t.Fatal(err)
	}
	operator := &fakeSpatialOperator{result: core.Layer{Features: []core.Feature{{ID: 3}}}}
	if err := service.ApplySpatialOperation(context.Background(), operator, "intersect", "left", "right", "result", 0); err != nil {
		t.Fatal(err)
	}
	project := service.Project()
	if len(project.Layers) != 3 || project.Layers[2].Name != "result" || len(project.Layers[2].Features) != 1 {
		t.Fatalf("unexpected project after operation: %#v", project.Layers)
	}
}

func TestProjectServiceSpatialFailureLeavesProjectUnchanged(t *testing.T) {
	service := NewProjectService("demo", core.CRS{})
	if err := service.BeginEdit(); err != nil {
		t.Fatal(err)
	}
	if err := service.AddLayer(core.Layer{Name: "left"}); err != nil {
		t.Fatal(err)
	}
	if err := service.Commit(); err != nil {
		t.Fatal(err)
	}
	operator := &fakeSpatialOperator{err: errors.New("GEOS failure")}
	if err := service.ApplySpatialOperation(context.Background(), operator, "buffer", "left", "", "result", 1); err == nil {
		t.Fatal("expected operation failure")
	}
	if len(service.Project().Layers) != 1 {
		t.Fatal("failed spatial operation changed project")
	}
}
