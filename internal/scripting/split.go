package scripting

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuin/gopher-lua"
	"gogis/internal/commands"
	"gogis/internal/core"
)

// FilterLayerByLua applies a boolean expression to feature properties.
// Example: return feature.grade == "A". The source remains untouched.
func FilterLayerByLua(ctx context.Context, layer core.Layer, script string) (core.Layer, error) {
	program, err := CompileLabelProgram(script)
	if err != nil {
		return core.Layer{}, err
	}
	defer program.Close()
	return commands.FilterLayerByPredicate(ctx, layer, func(ctx context.Context, feature core.Feature) (bool, error) {
		value, err := program.Evaluate(ctx, feature.Properties)
		if err != nil {
			return false, err
		}
		if value.Type() != lua.LTBool {
			return false, fmt.Errorf("Lua filter must return boolean, got %s", value.Type())
		}
		return lua.LVAsBool(value), nil
	})
}

// SplitLayerByLua groups features by a Lua-returned identifier. nil and an
// empty string omit a feature. The group identifier becomes the layer suffix.
func SplitLayerByLua(ctx context.Context, layer core.Layer, prefix, script string, maxGroups int) ([]core.Layer, error) {
	if strings.TrimSpace(prefix) == "" {
		return nil, fmt.Errorf("result layer prefix is required")
	}
	if maxGroups <= 0 {
		maxGroups = 64
	}
	program, err := CompileLabelProgram(script)
	if err != nil {
		return nil, err
	}
	defer program.Close()
	groups := make([]core.Layer, 0)
	indices := make(map[string]int)
	for _, feature := range layer.Features {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		value, err := program.Evaluate(ctx, feature.Properties)
		if err != nil {
			return nil, fmt.Errorf("feature %d: %w", feature.ID, err)
		}
		if value == lua.LNil {
			continue
		}
		if value.Type() != lua.LTString && value.Type() != lua.LTNumber {
			return nil, fmt.Errorf("feature %d: Lua split must return text, number, or nil", feature.ID)
		}
		identifier := strings.TrimSpace(value.String())
		if identifier == "" {
			continue
		}
		if len(identifier) > 128 || strings.ContainsAny(identifier, "/\\\r\n\x00") {
			return nil, fmt.Errorf("feature %d: invalid split identifier", feature.ID)
		}
		index, exists := indices[identifier]
		if !exists {
			if len(groups) >= maxGroups {
				return nil, fmt.Errorf("Lua split exceeds %d result layers", maxGroups)
			}
			index = len(groups)
			indices[identifier] = index
			result := layer
			result.Name = prefix + "_" + identifier
			result.DisplayName = result.Name
			result.SourcePath, result.SourceLayerName, result.SourceEncoding, result.SourceCRS = "", "", "", ""
			result.Visible, result.Editable, result.GeometryLocked = true, true, true
			result.Fields = append([]core.Field(nil), layer.Fields...)
			result.Features = nil
			groups = append(groups, result)
		}
		groups[index].Features = append(groups[index].Features, feature)
	}
	return groups, nil
}
