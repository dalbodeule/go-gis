package render

import "sync"

// LayerVisibility is the render-side visibility contract used by layer tree
// adapters. Unknown layers are hidden until explicitly added.
type LayerVisibility struct {
	mu      sync.RWMutex
	order   []string
	visible map[string]bool
}

// NewLayerVisibility creates a visibility state with the supplied layers on.
func NewLayerVisibility(layers ...string) *LayerVisibility {
	state := &LayerVisibility{visible: make(map[string]bool)}
	for _, layer := range layers {
		state.Add(layer)
	}
	return state
}

// Add registers a layer as visible. It returns false when the layer already
// exists.
func (s *LayerVisibility) Add(layer string) bool {
	if layer == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.visible[layer]; exists {
		return false
	}
	s.visible[layer] = true
	s.order = append(s.order, layer)
	return true
}

// Set changes visibility and reports whether the layer was registered.
func (s *LayerVisibility) Set(layer string, visible bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.visible[layer]; !exists {
		return false
	}
	s.visible[layer] = visible
	return true
}

// IsVisible reports whether a registered layer is currently visible.
func (s *LayerVisibility) IsVisible(layer string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.visible[layer]
}

// VisibleLayers returns registered visible layers in layer-tree order.
func (s *LayerVisibility) VisibleLayers() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]string, 0, len(s.order))
	for _, layer := range s.order {
		if s.visible[layer] {
			result = append(result, layer)
		}
	}
	return result
}

// FilterChunkKeys drops chunks belonging to hidden or unknown layers.
func (s *LayerVisibility) FilterChunkKeys(keys []ChunkKey) []ChunkKey {
	s.mu.RLock()
	defer s.mu.RUnlock()
	filtered := make([]ChunkKey, 0, len(keys))
	for _, key := range keys {
		if s.visible[key.Layer] {
			filtered = append(filtered, key)
		}
	}
	return filtered
}
