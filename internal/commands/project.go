// Package commands contains operations shared by GUI, CLI, and scripting.
package commands

import (
	"context"
	"errors"
	"fmt"
	"maps"

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
	project             *core.Project
	draft               *core.Project
	draftFeatureCopies  map[int]bool
	draftPropertyCopies map[int]map[int]bool
	featureIndex        map[string]map[uint64]int
	indexedProject      *core.Project
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

// LayerNames returns only layer identity metadata. UI layer trees should use
// this instead of Project when they do not need geometry or properties.
func (s *ProjectService) LayerNames() []string {
	names := make([]string, len(s.project.Layers))
	for index, layer := range s.project.Layers {
		names[index] = layer.Name
	}
	return names
}

// Layer returns a detached snapshot of one named layer without cloning other
// project layers. Read-heavy UI paths should use this instead of Project when
// they only need one layer.
func (s *ProjectService) Layer(name string) (core.Layer, bool) {
	for _, layer := range s.project.Layers {
		if layer.Name == name {
			return layer.Clone(), true
		}
	}
	return core.Layer{}, false
}

// LayerAttributes returns a detached layer snapshot containing only schema
// and properties. Geometry is intentionally omitted for attribute-table paths.
func (s *ProjectService) LayerAttributes(name string) (core.Layer, bool) {
	for _, layer := range s.project.Layers {
		if layer.Name != name {
			continue
		}
		result := core.Layer{Name: layer.Name, CRS: layer.CRS, Editable: layer.Editable, Fields: append([]core.Field(nil), layer.Fields...)}
		result.Features = make([]core.Feature, len(layer.Features))
		for index, feature := range layer.Features {
			result.Features[index] = core.Feature{ID: feature.ID, Properties: maps.Clone(feature.Properties)}
		}
		return result, true
	}
	return core.Layer{}, false
}

// LayerAttributePage returns only one detached attribute-table page. Geometry
// is omitted and properties are cloned only for the requested rows.
func (s *ProjectService) LayerAttributePage(name string, offset, limit int) (core.Layer, int, bool) {
	return s.layerAttributePage(name, offset, limit, true)
}

// LayerAttributePageOwned returns a read-only page view whose feature property
// maps remain owned by the service. Callers must not mutate the returned maps;
// this avoids a second clone in presentation-only UI paths.
func (s *ProjectService) LayerAttributePageOwned(name string, offset, limit int) (core.Layer, int, bool) {
	return s.layerAttributePage(name, offset, limit, false)
}

func (s *ProjectService) layerAttributePage(name string, offset, limit int, clonePropertyMaps bool) (core.Layer, int, bool) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		return core.Layer{}, 0, false
	}
	for _, layer := range s.project.Layers {
		if layer.Name != name {
			continue
		}
		total := len(layer.Features)
		if offset > total {
			offset = total
		}
		end := offset + limit
		if end > total {
			end = total
		}
		result := core.Layer{Name: layer.Name, CRS: layer.CRS, Editable: layer.Editable, Fields: append([]core.Field(nil), layer.Fields...)}
		result.Features = make([]core.Feature, end-offset)
		for index, feature := range layer.Features[offset:end] {
			properties := feature.Properties
			if clonePropertyMaps {
				properties = maps.Clone(properties)
			}
			result.Features[index] = core.Feature{ID: feature.ID, Properties: properties}
		}
		return result, total, true
	}
	return core.Layer{}, 0, false
}

// HasFeature checks feature existence without cloning geometry or properties.
func (s *ProjectService) HasFeature(layerName string, featureID uint64) bool {
	index, ok := s.featureIndexForLayer(layerName)
	if !ok {
		return false
	}
	_, ok = index[featureID]
	return ok
}

// FeatureProperty returns one property without cloning the containing feature.
func (s *ProjectService) FeatureProperty(layerName string, featureID uint64, propertyName string) (any, bool) {
	for _, layer := range s.project.Layers {
		if layer.Name != layerName {
			continue
		}
		index, ok := s.featureIndexForLayer(layerName)
		if !ok {
			return nil, false
		}
		featureIndex, ok := index[featureID]
		if !ok {
			return nil, false
		}
		value, ok := layer.Features[featureIndex].Properties[propertyName]
		return value, ok
	}
	return nil, false
}

func (s *ProjectService) invalidateFeatureIndex() {
	s.featureIndex = nil
	s.indexedProject = nil
}

func (s *ProjectService) featureIndexForLayer(layerName string) (map[uint64]int, bool) {
	if s.indexedProject != s.project {
		s.featureIndex = make(map[string]map[uint64]int, len(s.project.Layers))
		s.indexedProject = s.project
	}
	if index, ok := s.featureIndex[layerName]; ok {
		return index, true
	}
	for _, layer := range s.project.Layers {
		if layer.Name != layerName {
			continue
		}
		index := make(map[uint64]int, len(layer.Features))
		for featureIndex, feature := range layer.Features {
			if _, exists := index[feature.ID]; !exists {
				index[feature.ID] = featureIndex
			}
		}
		s.featureIndex[layerName] = index
		return index, true
	}
	return nil, false
}

// BeginEdit starts an atomic edit transaction.
func (s *ProjectService) BeginEdit() error {
	if s.draft != nil {
		return errors.New("edit transaction already active")
	}
	// Start with detached project/layer headers. Feature slices, geometry
	// values, and property maps remain shared until a mutation targets them;
	// mutation helpers copy the affected layer/feature first. This preserves
	// rollback isolation without cloning an entire large dataset up front.
	draft := *s.project
	draft.Layers = append([]core.Layer(nil), s.project.Layers...)
	s.draft = &draft
	s.draftFeatureCopies = make(map[int]bool)
	s.draftPropertyCopies = make(map[int]map[int]bool)
	return nil
}

// Commit publishes the current edit transaction.
func (s *ProjectService) Commit() error {
	if s.draft == nil {
		return ErrEditNotActive
	}
	s.project = s.draft
	s.draft = nil
	s.draftFeatureCopies = nil
	s.draftPropertyCopies = nil
	s.invalidateFeatureIndex()
	return nil
}

// Rollback discards the current edit transaction.
func (s *ProjectService) Rollback() error {
	if s.draft == nil {
		return ErrEditNotActive
	}
	s.draft = nil
	s.draftFeatureCopies = nil
	s.draftPropertyCopies = nil
	return nil
}

func (s *ProjectService) ensureDraftFeatureSlice(layerIndex int) {
	if s.draftFeatureCopies[layerIndex] {
		return
	}
	layer := &s.draft.Layers[layerIndex]
	layer.Features = append([]core.Feature(nil), layer.Features...)
	s.draftFeatureCopies[layerIndex] = true
}

func (s *ProjectService) ensureDraftFeatureProperties(layerIndex, featureIndex int) {
	copied := s.draftPropertyCopies[layerIndex]
	if copied == nil {
		copied = make(map[int]bool)
		s.draftPropertyCopies[layerIndex] = copied
	}
	if copied[featureIndex] {
		return
	}
	feature := &s.draft.Layers[layerIndex].Features[featureIndex]
	feature.Properties = maps.Clone(feature.Properties)
	if feature.Properties == nil {
		feature.Properties = map[string]any{}
	}
	copied[featureIndex] = true
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
		s.ensureDraftFeatureSlice(layerIndex)
		layer = &s.draft.Layers[layerIndex]
		for _, existing := range layer.Features {
			if existing.ID == feature.ID {
				return fmt.Errorf("feature %d already exists in layer %q", feature.ID, layerName)
			}
		}
		featureIndex := len(layer.Features)
		layer.Features = append(layer.Features, feature.Clone())
		copied := s.draftPropertyCopies[layerIndex]
		if copied == nil {
			copied = make(map[int]bool)
			s.draftPropertyCopies[layerIndex] = copied
		}
		copied[featureIndex] = true
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
		if featureIndex, indexed := s.featureIndexForLayer(layerName); indexed {
			if index, exists := featureIndex[featureID]; exists && index < len(layer.Features) &&
				layer.Features[index].ID == featureID {
				s.ensureDraftFeatureSlice(layerIndex)
				layer = &s.draft.Layers[layerIndex]
				s.ensureDraftFeatureProperties(layerIndex, index)
				feature := &layer.Features[index]
				feature.Properties[field] = value
				return nil
			}
		}
		for featureIndex := range layer.Features {
			feature := &layer.Features[featureIndex]
			if feature.ID == featureID {
				s.ensureDraftFeatureSlice(layerIndex)
				layer = &s.draft.Layers[layerIndex]
				s.ensureDraftFeatureProperties(layerIndex, featureIndex)
				feature = &layer.Features[featureIndex]
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
	if len(s.project.Layers) == 0 {
		return ErrNoLayers
	}
	if layerName == "" {
		if len(s.project.Layers) != 1 {
			return errors.New("layer name is required when project has multiple layers")
		}
		layerName = s.project.Layers[0].Name
	}
	for _, layer := range s.project.Layers {
		if layer.Name == layerName {
			return writer.Write(ctx, destination, layer.Clone())
		}
	}
	return fmt.Errorf("%w: %s", ErrLayerMissing, layerName)
}
