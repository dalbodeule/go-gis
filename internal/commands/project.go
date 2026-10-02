// Package commands contains operations shared by GUI, CLI, and scripting.
package commands

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	"gogis/internal/core"
)

var (
	ErrEditNotActive = errors.New("edit transaction is not active")
	ErrLayerExists   = errors.New("layer already exists")
	ErrLayerMissing  = errors.New("layer not found")
	ErrNoLayers      = errors.New("project has no layers")
)

const maxAttributePageSize = 1_000

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

// LayerCollectionWriter persists a project as one multi-layer dataset.
type LayerCollectionWriter interface {
	WriteLayers(ctx context.Context, destination string, layers []core.Layer) error
}

// NewProjectService creates a service with an empty project.
func NewProjectService(name string, crs core.CRS) *ProjectService {
	project := core.Project{Name: name, CRS: crs}
	return &ProjectService{project: &project}
}

// NewProjectServiceWithLayers adopts loader-owned layer snapshots without
// cloning every feature geometry and property map. The caller must not mutate
// the supplied layers after the call; ProjectService owns their feature data.
func NewProjectServiceWithLayers(name string, crs core.CRS, layers []core.Layer) (*ProjectService, error) {
	ownedLayers := append([]core.Layer(nil), layers...)
	seen := make(map[string]struct{}, len(ownedLayers))
	for _, layer := range ownedLayers {
		if layer.Name == "" {
			return nil, errors.New("layer name is required")
		}
		if _, exists := seen[layer.Name]; exists {
			return nil, fmt.Errorf("%w: %s", ErrLayerExists, layer.Name)
		}
		seen[layer.Name] = struct{}{}
	}
	project := core.Project{Name: name, CRS: crs, Layers: ownedLayers}
	return &ProjectService{project: &project}, nil
}

// Project returns the committed project snapshot.
func (s *ProjectService) Project() core.Project {
	return s.project.Clone()
}

// ProjectRenderSnapshot copies project, layer, field, and feature headers while
// sharing immutable geometry/property snapshots. Callers may update feature
// headers and presentation metadata, but must not mutate shared geometry or
// property values. ProjectService edit transactions use copy-on-write, so edits
// made after this snapshot remain isolated.
func (s *ProjectService) ProjectRenderSnapshot() core.Project {
	result := *s.project
	result.Layers = make([]core.Layer, len(s.project.Layers))
	for index, layer := range s.project.Layers {
		result.Layers[index] = layerRenderSnapshot(layer)
	}
	return result
}

// ProjectInfo returns project identity without cloning its layer data.
func (s *ProjectService) ProjectInfo() (string, core.CRS) {
	return s.project.Name, s.project.CRS
}

// SetProjectInfo updates workspace identity without touching layer snapshots.
func (s *ProjectService) SetProjectInfo(name string, crs core.CRS) {
	s.project.Name = name
	s.project.CRS = crs
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

// RenameLayer changes the project display name without changing the source
// dataset's internal layer identity.
func (s *ProjectService) RenameLayer(name, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return fmt.Errorf("layer name must not be empty")
	}
	index := -1
	for i := range s.project.Layers {
		if s.project.Layers[i].Name == name {
			index = i
			continue
		}
		otherName := s.project.Layers[i].DisplayName
		if otherName == "" {
			otherName = s.project.Layers[i].Name
		}
		if strings.EqualFold(otherName, newName) {
			return fmt.Errorf("%w: %s", ErrLayerExists, newName)
		}
	}
	if index < 0 {
		return ErrLayerMissing
	}
	s.project.Layers[index].DisplayName = newName
	return nil
}

// UpdateLayerSettings validates and replaces one layer's source and display
// settings. Dataset reopening and renderer refresh remain runtime concerns.
func (s *ProjectService) UpdateLayerSettings(name string, updated core.Layer) error {
	if updated.Name != "" && updated.Name != name {
		return fmt.Errorf("layer identity cannot be changed through settings")
	}
	if err := updated.Style.Validate(); err != nil {
		return err
	}
	if err := updated.Labels.Validate(); err != nil {
		return err
	}
	for index := range s.project.Layers {
		if s.project.Layers[index].Name != name {
			continue
		}
		s.project.Layers[index].SourcePath = updated.SourcePath
		s.project.Layers[index].DisplayName = updated.DisplayName
		s.project.Layers[index].SourceLayerName = updated.SourceLayerName
		s.project.Layers[index].SourceEncoding = updated.SourceEncoding
		s.project.Layers[index].SourceCRS = updated.SourceCRS
		s.project.Layers[index].Visible = updated.Visible
		s.project.Layers[index].Style = updated.Style
		s.project.Layers[index].Labels = updated.Labels
		return nil
	}
	return ErrLayerMissing
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

// LayerProperties returns one lightweight settings snapshot without cloning
// feature geometry or attribute maps.
func (s *ProjectService) LayerProperties(name string) (core.Layer, bool) {
	for _, layer := range s.project.Layers {
		if layer.Name == name {
			return core.Layer{
				Name: layer.Name, DisplayName: layer.DisplayName, SourcePath: layer.SourcePath,
				SourceLayerName: layer.SourceLayerName, SourceEncoding: layer.SourceEncoding, SourceCRS: layer.SourceCRS,
				CRS: layer.CRS, Editable: layer.Editable, Visible: layer.Visible,
				Style: layer.Style, Labels: layer.Labels,
			}, true
		}
	}
	return core.Layer{}, false
}

// ProjectLayerProperties returns only metadata and presentation settings for
// all layers, avoiding copies of large feature snapshots during workspace IO.
func (s *ProjectService) ProjectLayerProperties() []core.Layer {
	result := make([]core.Layer, len(s.project.Layers))
	for index, layer := range s.project.Layers {
		result[index] = core.Layer{
			Name: layer.Name, DisplayName: layer.DisplayName, SourcePath: layer.SourcePath,
			SourceLayerName: layer.SourceLayerName, SourceEncoding: layer.SourceEncoding, SourceCRS: layer.SourceCRS,
			CRS: layer.CRS, Editable: layer.Editable, Visible: layer.Visible,
			Style: layer.Style, Labels: layer.Labels,
		}
	}
	return result
}

// ProjectLayersExcept returns detached layer snapshots except for one layer.
// Runtime source replacement uses it to reload a single dataset without
// reopening unrelated layers or discarding their in-memory edits.
func (s *ProjectService) ProjectLayersExcept(excludedName string) []core.Layer {
	result := make([]core.Layer, 0, len(s.project.Layers))
	for _, layer := range s.project.Layers {
		if layer.Name == excludedName {
			continue
		}
		result = append(result, layer.Clone())
	}
	return result
}

// ProjectLayerRenderSnapshot copies layer metadata and feature headers while
// sharing immutable geometry/property snapshots. Render preparation may update
// the copied Feature.Label pointers, but callers must not mutate geometries or
// property maps. This avoids duplicating every WKB when rebuilding one layer.
func (s *ProjectService) ProjectLayerRenderSnapshot(name string) (core.Layer, bool) {
	for _, layer := range s.project.Layers {
		if layer.Name != name {
			continue
		}
		return layerRenderSnapshot(layer), true
	}
	return core.Layer{}, false
}

func layerRenderSnapshot(layer core.Layer) core.Layer {
	result := layer
	result.Fields = append([]core.Field(nil), layer.Fields...)
	result.Features = append([]core.Feature(nil), layer.Features...)
	labelCount := 0
	for _, feature := range layer.Features {
		if feature.Label == nil {
			continue
		}
		labelCount++
	}
	if labelCount == 0 {
		return result
	}
	labelCopies := make([]core.Label, labelCount)
	labelIndex := 0
	for featureIndex, feature := range layer.Features {
		if feature.Label == nil {
			continue
		}
		labelCopies[labelIndex] = *feature.Label
		result.Features[featureIndex].Label = &labelCopies[labelIndex]
		labelIndex++
	}
	return result
}

// LayerEditable reports whether a layer is enabled for geometry edits.
func (s *ProjectService) LayerEditable(name string) bool {
	for _, layer := range s.project.Layers {
		if layer.Name == name {
			return layer.Editable
		}
	}
	return false
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
	if limit <= 0 || limit > maxAttributePageSize {
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
		end := total
		if limit <= total-offset {
			end = offset + limit
		} else {
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

// FeatureGeometry returns a detached geometry snapshot for editor and renderer
// consumers. The caller may safely retain it across a subsequent edit.
func (s *ProjectService) FeatureGeometry(layerName string, featureID uint64) (core.Geometry, bool) {
	for _, layer := range s.project.Layers {
		if layer.Name != layerName {
			continue
		}
		index, ok := s.featureIndexForLayer(layerName)
		if !ok {
			return nil, false
		}
		featureIndex, ok := index[featureID]
		if !ok || featureIndex >= len(layer.Features) {
			return nil, false
		}
		geometry := layer.Features[featureIndex].Geometry
		if geometry == nil {
			return nil, false
		}
		return geometry.Clone(), true
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

// SetFeatureGeometry replaces one feature's geometry within the active edit.
// The supplied geometry is cloned so failed transactions and caller mutation
// cannot alter the committed snapshot.
func (s *ProjectService) SetFeatureGeometry(layerName string, featureID uint64, geometry core.Geometry) error {
	if s.draft == nil {
		return ErrEditNotActive
	}
	if geometry == nil {
		return errors.New("feature geometry is required")
	}
	for layerIndex := range s.draft.Layers {
		layer := &s.draft.Layers[layerIndex]
		if layer.Name != layerName {
			continue
		}
		if !layer.Editable {
			return fmt.Errorf("layer %q is not editable", layerName)
		}
		index, indexed := s.featureIndexForLayer(layerName)
		featureIndex, exists := index[featureID]
		if !indexed || !exists || featureIndex >= len(layer.Features) || layer.Features[featureIndex].ID != featureID {
			return fmt.Errorf("feature %d not found in layer %q", featureID, layerName)
		}
		s.ensureDraftFeatureSlice(layerIndex)
		s.draft.Layers[layerIndex].Features[featureIndex].Geometry = geometry.Clone()
		return nil
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

// SaveAllLayers persists a project as a dataset. Writers without collection
// support can still save a single-layer project.
func (s *ProjectService) SaveAllLayers(ctx context.Context, writer LayerWriter, destination string) error {
	if writer == nil {
		return errors.New("layer writer is required")
	}
	project := s.Project()
	if len(project.Layers) == 0 {
		return ErrNoLayers
	}
	if collectionWriter, ok := writer.(LayerCollectionWriter); ok {
		return collectionWriter.WriteLayers(ctx, destination, project.Layers)
	}
	if len(project.Layers) != 1 {
		return errors.New("writer does not support multi-layer datasets")
	}
	return writer.Write(ctx, destination, project.Layers[0])
}
