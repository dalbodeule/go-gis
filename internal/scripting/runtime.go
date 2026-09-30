// Package scripting exposes a deliberately small, safe Lua API.
package scripting

import (
	"context"
	"fmt"
	"os"
	"sync"

	"gogis/internal/commands"
	"gogis/internal/core"

	"github.com/yuin/gopher-lua"
)

// Exporter is the narrow output capability exposed to scripts.
type Exporter interface {
	Export(ctx context.Context, destination string, layer core.Layer, profile string) error
}

// Runtime runs Lua scripts without exposing internal pointers or UI objects.
type Runtime struct {
	state    *lua.LState
	service  *commands.ProjectService
	exporter Exporter
	spatial  commands.SpatialOperator
	mu       sync.Mutex
	runCtx   context.Context
}

// NewRuntime creates a runtime with the minimum public API. An optional
// spatial operator enables the shared GEOS-backed spatial command in Lua.
func NewRuntime(service *commands.ProjectService, exporter Exporter, spatial ...commands.SpatialOperator) *Runtime {
	state := lua.NewState(lua.Options{SkipOpenLibs: true})
	// Explicit allow-list: scripts get language helpers and deterministic
	// string/math/table operations, but no filesystem, process, module, debug,
	// coroutine, or channel capabilities.
	for _, open := range []func(*lua.LState) int{lua.OpenBase, lua.OpenTable, lua.OpenString, lua.OpenMath} {
		if results := open(state); results > 0 {
			state.Pop(results)
		}
	}
	for _, name := range []string{
		"dofile", "loadfile", "load", "loadstring", "require", "module",
		"print", "collectgarbage", "newproxy",
	} {
		state.SetGlobal(name, lua.LNil)
	}
	var spatialOperator commands.SpatialOperator
	if len(spatial) > 0 {
		spatialOperator = spatial[0]
	}
	runtime := &Runtime{state: state, service: service, exporter: exporter, spatial: spatialOperator}
	runtime.state.SetGlobal("gogis", runtime.state.SetFuncs(runtime.state.NewTable(), map[string]lua.LGFunction{
		"layers":       runtime.layers,
		"set_property": runtime.setProperty,
		"export_dxf":   runtime.exportDXF,
		"spatial":      runtime.spatialOperation,
		"filter":       runtime.filter,
		"label":        runtime.label,
	}))
	return runtime
}

func (r *Runtime) filter(state *lua.LState) int {
	source := state.CheckString(1)
	field := state.CheckString(2)
	expected := state.CheckString(3)
	result := state.CheckString(4)
	ctx := r.runCtx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := r.service.FilterProjectLayer(ctx, source, field, expected, result); err != nil {
		state.RaiseError("filter: %v", err)
	}
	return 0
}

func (r *Runtime) label(state *lua.LState) int {
	source := state.CheckString(1)
	field := state.CheckString(2)
	result := state.CheckString(3)
	height := float64(state.OptNumber(4, 1))
	style := state.OptString(5, "")
	ctx := r.runCtx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := r.service.LabelProjectLayer(ctx, source, field, result, height, style); err != nil {
		state.RaiseError("label: %v", err)
	}
	return 0
}

func (r *Runtime) spatialOperation(state *lua.LState) int {
	if r.spatial == nil {
		state.RaiseError("spatial operator is not configured")
		return 0
	}
	operation := state.CheckString(1)
	left := state.CheckString(2)
	right := state.OptString(3, "")
	result := state.CheckString(4)
	distance := state.OptNumber(5, 0)
	ctx := r.runCtx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := r.service.ApplySpatialOperation(ctx, r.spatial, operation, left, right, result, float64(distance)); err != nil {
		state.RaiseError("spatial operation: %v", err)
	}
	return 0
}

// Close releases the Lua state.
func (r *Runtime) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state.Close()
}

// Run executes a script with a cancellation check before entry.
func (r *Runtime) Run(ctx context.Context, script string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runCtx = ctx
	defer func() { r.runCtx = nil }()
	r.state.SetContext(ctx)
	defer r.state.RemoveContext()
	return r.state.DoString(script)
}

func (r *Runtime) layers(state *lua.LState) int {
	result := state.NewTable()
	for index, name := range r.service.LayerNames() {
		result.RawSetInt(index+1, lua.LString(name))
	}
	state.Push(result)
	return 1
}

func (r *Runtime) setProperty(state *lua.LState) int {
	layerName := state.CheckString(1)
	featureID := uint64(state.CheckInt64(2))
	field := state.CheckString(3)
	value := luaValue(state.Get(4))
	if err := r.service.BeginEdit(); err != nil {
		state.RaiseError("begin edit: %v", err)
		return 0
	}
	if err := r.service.SetFeatureProperty(layerName, featureID, field, value); err != nil {
		_ = r.service.Rollback()
		state.RaiseError("set property: %v", err)
		return 0
	}
	if err := r.service.Commit(); err != nil {
		state.RaiseError("commit edit: %v", err)
	}
	return 0
}

func (r *Runtime) exportDXF(state *lua.LState) int {
	if r.exporter == nil {
		state.RaiseError("DXF exporter is not configured")
		return 0
	}
	destination := state.CheckString(1)
	layerName := state.CheckString(2)
	profile := state.OptString(3, "ares-utf8")
	ctx := r.runCtx
	if ctx == nil {
		ctx = context.Background()
	}
	layer, ok := r.service.Layer(layerName)
	if !ok {
		state.RaiseError("layer %q not found", layerName)
		return 0
	}
	if err := r.exporter.Export(ctx, destination, layer, profile); err != nil {
		state.RaiseError("export DXF: %v", err)
	}
	return 0
}

func luaValue(value lua.LValue) any {
	switch typed := value.(type) {
	case *lua.LNilType:
		return nil
	case lua.LString:
		return string(typed)
	case lua.LNumber:
		return float64(typed)
	case lua.LBool:
		return bool(typed)
	default:
		return fmt.Sprint(value)
	}
}

// RunFile executes a script file using the supplied context.
func (r *Runtime) RunFile(ctx context.Context, path string) error {
	script, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return r.Run(ctx, string(script))
}
