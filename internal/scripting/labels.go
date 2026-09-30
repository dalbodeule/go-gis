package scripting

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/yuin/gopher-lua"
)

const maxLabelScriptBytes = 64 << 10

// LabelProgram is a restricted, reusable Lua expression for one layer's label
// text or display rule. Scripts read feature properties through `feature` and
// return a string (text) or boolean (rule). Filesystem and process libraries
// are unavailable.
type LabelProgram struct {
	mu    sync.Mutex
	state *lua.LState
	fn    *lua.LFunction
}

func CompileLabelProgram(script string) (*LabelProgram, error) {
	if len(script) == 0 || len(script) > maxLabelScriptBytes {
		return nil, fmt.Errorf("label script size must be between 1 and %d bytes", maxLabelScriptBytes)
	}
	state := lua.NewState(lua.Options{SkipOpenLibs: true, RegistryMaxSize: 32 * 1024})
	for _, open := range []func(*lua.LState) int{lua.OpenBase, lua.OpenTable, lua.OpenString, lua.OpenMath} {
		if results := open(state); results > 0 {
			state.Pop(results)
		}
	}
	for _, name := range []string{"dofile", "loadfile", "load", "loadstring", "require", "module", "print", "collectgarbage", "newproxy"} {
		state.SetGlobal(name, lua.LNil)
	}
	state.SetMx(8 << 20)
	function, err := state.LoadString(script)
	if err != nil {
		state.Close()
		return nil, fmt.Errorf("compile label script: %w", err)
	}
	return &LabelProgram{state: state, fn: function}, nil
}

func (p *LabelProgram) Evaluate(ctx context.Context, properties map[string]any) (lua.LValue, error) {
	if p == nil || p.state == nil {
		return nil, errors.New("label program is closed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state.SetContext(ctx)
	feature := p.state.NewTable()
	for key, value := range properties {
		feature.RawSetString(key, labelLuaValue(value))
	}
	p.state.SetGlobal("feature", feature)
	p.state.Push(p.fn)
	err := p.state.PCall(0, 1, nil)
	if err != nil {
		p.state.SetContext(context.Background())
		return nil, fmt.Errorf("evaluate label script: %w", err)
	}
	result := p.state.Get(-1)
	p.state.Pop(1)
	p.state.SetContext(context.Background())
	return result, nil
}

func (p *LabelProgram) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != nil {
		p.state.Close()
		p.state = nil
		p.fn = nil
	}
}

func (p *LabelProgram) EvaluateText(ctx context.Context, properties map[string]any) (string, error) {
	value, err := p.Evaluate(ctx, properties)
	if err != nil {
		return "", err
	}
	switch value.Type() {
	case lua.LTString:
		text := value.String()
		if len(text) > maxLabelScriptBytes {
			return "", fmt.Errorf("label script result exceeds %d bytes", maxLabelScriptBytes)
		}
		return text, nil
	case lua.LTNumber:
		return value.String(), nil
	case lua.LTNil:
		return "", nil
	default:
		return "", fmt.Errorf("label script must return string or number, got %s", value.Type())
	}
}

func (p *LabelProgram) EvaluateRule(ctx context.Context, properties map[string]any) (bool, error) {
	value, err := p.Evaluate(ctx, properties)
	if err != nil {
		return false, err
	}
	if value.Type() != lua.LTBool {
		return false, fmt.Errorf("label rule must return boolean, got %s", value.Type())
	}
	return lua.LVAsBool(value), nil
}

func labelLuaValue(value any) lua.LValue {
	switch typed := value.(type) {
	case nil:
		return lua.LNil
	case string:
		return lua.LString(typed)
	case bool:
		return lua.LBool(typed)
	case int:
		return lua.LNumber(typed)
	case int8:
		return lua.LNumber(typed)
	case int16:
		return lua.LNumber(typed)
	case int32:
		return lua.LNumber(typed)
	case int64:
		return lua.LNumber(typed)
	case uint:
		return lua.LNumber(typed)
	case uint8:
		return lua.LNumber(typed)
	case uint16:
		return lua.LNumber(typed)
	case uint32:
		return lua.LNumber(typed)
	case uint64:
		return lua.LNumber(typed)
	case float32:
		return lua.LNumber(typed)
	case float64:
		return lua.LNumber(typed)
	default:
		return lua.LString(strings.TrimSpace(fmt.Sprint(typed)))
	}
}
