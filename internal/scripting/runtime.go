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
	mu       sync.Mutex
}

// NewRuntime creates a runtime with the minimum public API.
func NewRuntime(service *commands.ProjectService, exporter Exporter) *Runtime {
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
	runtime := &Runtime{state: state, service: service, exporter: exporter}
	runtime.state.SetGlobal("gogis", runtime.state.SetFuncs(runtime.state.NewTable(), map[string]lua.LGFunction{
		"layers":       runtime.layers,
		"set_property": runtime.setProperty,
		"export_dxf":   runtime.exportDXF,
	}))
	return runtime
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
	r.state.SetContext(ctx)
	defer r.state.RemoveContext()
	return r.state.DoString(script)
}

func (r *Runtime) layers(state *lua.LState) int {
	project := r.service.Project()
	result := state.NewTable()
	for index, layer := range project.Layers {
		result.RawSetInt(index+1, lua.LString(layer.Name))
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
	for _, layer := range r.service.Project().Layers {
		if layer.Name == layerName {
			if err := r.exporter.Export(context.Background(), destination, layer, "ares-utf8"); err != nil {
				state.RaiseError("export DXF: %v", err)
			}
			return 0
		}
	}
	state.RaiseError("layer %q not found", layerName)
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
