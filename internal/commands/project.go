// Package commands contains operations shared by GUI, CLI, and scripting.
package commands

import (
	"context"
	"errors"
	"fmt"

	"gogis/internal/core"
)

var (
	ErrEditNotActive = errors.New("edit transaction is not active")
	ErrLayerExists   = errors.New("layer already exists")
	ErrLayerMissing  = errors.New("layer not found")
	ErrNoLayers      = errors.New("project has no layers")
)

// ProjectService owns project edits independently from any user interface.
type ProjectService struct {
	project *core.Project
	draft   *core.Project
}

// LayerWriter is the minimal persistence boundary used by the command layer.
// Native implementations such as drivers/gdal.Writer can be injected without
// coupling ProjectService to a file format or CGO package.
type LayerWriter interface {
	Write(ctx context.Context, destination string, layer core.Layer) error
}

// NewProjectService creates a service with an empty project.
func NewProjectService(name string, crs core.CRS) *ProjectService {
	project := core.Project{Name: name, CRS: crs}
	return &ProjectService{project: &project}
}

// Project returns the committed project snapshot.
func (s *ProjectService) Project() core.Project {
	return s.project.Clone()
}

// BeginEdit starts an atomic edit transaction.
func (s *ProjectService) BeginEdit() error {
	if s.draft != nil {
		return errors.New("edit transaction already active")
	}
	draft := s.project.Clone()
	s.draft = &draft
	return nil
}

// Commit publishes the current edit transaction.
func (s *ProjectService) Commit() error {
	if s.draft == nil {
		return ErrEditNotActive
	}
	s.project = s.draft
	s.draft = nil
	return nil
}

// Rollback discards the current edit transaction.
func (s *ProjectService) Rollback() error {
	if s.draft == nil {
		return ErrEditNotActive
	}
	s.draft = nil
	return nil
}

// AddLayer adds a layer to the active edit transaction.
func (s *ProjectService) AddLayer(layer core.Layer) error {
	if s.draft == nil {
		return ErrEditNotActive
	}
	if layer.Name == "" {
		return errors.New("layer name is required")
	}
	for _, existing := range s.draft.Layers {
		if existing.Name == layer.Name {
			return fmt.Errorf("%w: %s", ErrLayerExists, layer.Name)
		}
	}
	s.draft.Layers = append(s.draft.Layers, layer.Clone())
	return nil
}

// AddFeature adds a detached feature to an editable layer in the active edit.
func (s *ProjectService) AddFeature(layerName string, feature core.Feature) error {
	if s.draft == nil {
		return ErrEditNotActive
	}
	for layerIndex := range s.draft.Layers {
		layer := &s.draft.Layers[layerIndex]
		if layer.Name != layerName {
			continue
		}
		if !layer.Editable {
			return fmt.Errorf("layer %q is not editable", layerName)
		}
		for _, existing := range layer.Features {
			if existing.ID == feature.ID {
				return fmt.Errorf("feature %d already exists in layer %q", feature.ID, layerName)
			}
		}
		layer.Features = append(layer.Features, feature.Clone())
		return nil
	}
	return fmt.Errorf("%w: %s", ErrLayerMissing, layerName)
}

// SetFeatureProperty changes one property in a layer within the active edit.
func (s *ProjectService) SetFeatureProperty(layerName string, featureID uint64, field string, value any) error {
	if s.draft == nil {
		return ErrEditNotActive
	}
	for layerIndex := range s.draft.Layers {
		layer := &s.draft.Layers[layerIndex]
		if layer.Name != layerName {
			continue
		}
		if !layer.Editable {
			return fmt.Errorf("layer %q is not editable", layerName)
		}
		for featureIndex := range layer.Features {
			feature := &layer.Features[featureIndex]
			if feature.ID == featureID {
				if feature.Properties == nil {
					feature.Properties = map[string]any{}
				}
				feature.Properties[field] = value
				return nil
			}
		}
		return fmt.Errorf("feature %d not found in layer %q", featureID, layerName)
	}
	return fmt.Errorf("%w: %s", ErrLayerMissing, layerName)
}

// SaveLayer persists one committed layer through the injected writer. The
// committed snapshot is cloned before crossing the adapter boundary so a
// writer cannot mutate the service's project state.
func (s *ProjectService) SaveLayer(ctx context.Context, writer LayerWriter, destination, layerName string) error {
	if writer == nil {
		return errors.New("layer writer is required")
	}
	project := s.Project()
	if len(project.Layers) == 0 {
		return ErrNoLayers
	}
	if layerName == "" {
		if len(project.Layers) != 1 {
			return errors.New("layer name is required when project has multiple layers")
		}
		layerName = project.Layers[0].Name
	}
	for _, layer := range project.Layers {
		if layer.Name == layerName {
			return writer.Write(ctx, destination, layer.Clone())
		}
	}
	return fmt.Errorf("%w: %s", ErrLayerMissing, layerName)
}
