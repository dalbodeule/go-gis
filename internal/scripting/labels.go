package scripting

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yuin/gopher-lua"
)

const maxLabelScriptBytes = 64 << 10
const maxLuaGeneratedStringBytes = 1 << 20
const maxLabelEvaluationDuration = 100 * time.Millisecond
const maxLabelFeatureProperties = 65_536

// LabelProgram is a restricted, reusable Lua expression for one layer's label
// text or display rule. Scripts read feature properties through `feature` and
// return a string (text) or boolean (rule). Filesystem and process libraries
// are unavailable.
type LabelProgram struct {
	mu          sync.Mutex
	state       *lua.LState
	fn          *lua.LFunction
	feature     *lua.LTable
	featureData *lua.LTable
	featureKeys []string
}

func CompileLabelProgram(script string) (*LabelProgram, error) {
	if len(script) == 0 || len(script) > maxLabelScriptBytes {
		return nil, fmt.Errorf("label script size must be between 1 and %d bytes", maxLabelScriptBytes)
	}
	state := newLabelState()
	function, err := state.LoadString(script)
	if err != nil {
		state.Close()
		return nil, fmt.Errorf("compile label script: %w", err)
	}
	feature, featureData := newReadOnlyFeature(state)
	return &LabelProgram{state: state, fn: function, feature: feature, featureData: featureData}, nil
}

func newLabelState() *lua.LState {
	state := lua.NewState(lua.Options{SkipOpenLibs: true, RegistryMaxSize: 32 * 1024})
	for _, open := range []func(*lua.LState) int{lua.OpenBase, lua.OpenTable, lua.OpenString, lua.OpenMath} {
		if results := open(state); results > 0 {
			state.Pop(results)
		}
	}
	for _, name := range []string{"dofile", "loadfile", "load", "loadstring", "require", "module", "print", "collectgarbage", "newproxy", "rawset"} {
		state.SetGlobal(name, lua.LNil)
	}
	installBoundedLuaStringRep(state)
	return state
}

func installBoundedLuaStringRep(state *lua.LState) {
	stringLibrary, ok := state.GetGlobal("string").(*lua.LTable)
	if !ok {
		return
	}
	// GopherLua's pattern operations run to completion inside a Go library
	// call, outside the VM instruction/context checks. In particular gsub
	// repeatedly rebuilds the whole output for each match, allowing CPU and
	// allocation amplification that a context deadline cannot stop.
	for _, name := range []string{"byte", "find", "gmatch", "gsub", "match"} {
		stringLibrary.RawSetString(name, lua.LNil)
	}
	stringLibrary.RawSetString("rep", state.NewFunction(func(state *lua.LState) int {
		value := state.CheckString(1)
		count := state.CheckInt(2)
		if count < 0 {
			state.Push(lua.LString(""))
			return 1
		}
		if len(value) > 0 && count > maxLuaGeneratedStringBytes/len(value) {
			state.RaiseError("string.rep result exceeds the %d-byte sandbox limit", maxLuaGeneratedStringBytes)
			return 0
		}
		state.Push(lua.LString(strings.Repeat(value, count)))
		return 1
	}))
	stringLibrary.RawSetString("format", state.NewFunction(boundedLuaStringFormat))
	if tableLibrary, ok := state.GetGlobal("table").(*lua.LTable); ok {
		tableLibrary.RawSetString("concat", state.NewFunction(boundedLuaTableConcat))
	}
}

func boundedLuaTableConcat(state *lua.LState) int {
	table := state.CheckTable(1)
	separator := state.OptString(2, "")
	start := state.OptInt(3, 1)
	end := state.OptInt(4, table.Len())
	if state.GetTop() == 3 && (start < 1 || start > table.Len()) {
		state.Push(lua.LString(""))
		return 1
	}
	start = max(1, min(start, table.Len()))
	end = min(end, table.Len())
	if start > end {
		state.Push(lua.LString(""))
		return 1
	}
	length := 0
	for index := start; index <= end; index++ {
		if index&255 == 0 {
			if ctx := state.Context(); ctx != nil {
				if err := ctx.Err(); err != nil {
					state.RaiseError("table.concat canceled: %v", err)
					return 0
				}
			}
		}
		value := table.RawGetInt(index)
		if !lua.LVCanConvToString(value) {
			state.RaiseError("invalid value (%s) at index %d in table for concat", value.Type().String(), index)
			return 0
		}
		valueLength := len(lua.LVAsString(value))
		separatorLength := 0
		if index != end {
			separatorLength = len(separator)
		}
		if valueLength > maxLuaGeneratedStringBytes-length || separatorLength > maxLuaGeneratedStringBytes-length-valueLength {
			state.RaiseError("table.concat result exceeds the %d-byte sandbox limit", maxLuaGeneratedStringBytes)
			return 0
		}
		length += valueLength + separatorLength
	}
	var result strings.Builder
	result.Grow(length)
	for index := start; index <= end; index++ {
		if index != start {
			result.WriteString(separator)
		}
		result.WriteString(lua.LVAsString(table.RawGetInt(index)))
	}
	state.Push(lua.LString(result.String()))
	return 1
}

func boundedLuaStringFormat(state *lua.LState) int {
	format := state.CheckString(1)
	maximumSpecifier, err := luaFormatMaximumSpecifier(format)
	if err != nil {
		state.RaiseError("string.format: %v", err)
		return 0
	}
	placeholders := strings.Count(format, "%") - strings.Count(format, "%%")
	if placeholders < 0 {
		placeholders = 0
	}
	maximumArgumentString := 128
	arguments := make([]interface{}, state.GetTop()-1)
	for index := range arguments {
		value := state.Get(index + 2)
		arguments[index] = value
		if text, ok := value.(lua.LString); ok && len(text) > maximumArgumentString {
			maximumArgumentString = len(text)
		}
	}
	bound := uint64(len(format)) + uint64(placeholders)*uint64(4*maximumArgumentString+maximumSpecifier+128)
	if bound > maxLuaGeneratedStringBytes {
		state.RaiseError("string.format result may exceed the %d-byte sandbox limit", maxLuaGeneratedStringBytes)
		return 0
	}
	state.Push(lua.LString(fmt.Sprintf(format, arguments[:min(placeholders, len(arguments))]...)))
	return 1
}

func luaFormatMaximumSpecifier(format string) (int, error) {
	maximum := 0
	for index := 0; index < len(format); index++ {
		if format[index] != '%' || index+1 >= len(format) {
			continue
		}
		index++
		if format[index] == '%' {
			continue
		}
		for index < len(format) {
			current := format[index]
			if current == '*' {
				return 0, fmt.Errorf("dynamic width and precision are disabled")
			}
			if current >= '0' && current <= '9' {
				start := index
				for index+1 < len(format) && format[index+1] >= '0' && format[index+1] <= '9' {
					index++
				}
				value, err := strconv.Atoi(format[start : index+1])
				if err != nil || value > maxLuaGeneratedStringBytes {
					return 0, fmt.Errorf("width or precision exceeds the %d-byte sandbox limit", maxLuaGeneratedStringBytes)
				}
				maximum = max(maximum, value)
			}
			if (current >= 'a' && current <= 'z') || (current >= 'A' && current <= 'Z') {
				break
			}
			index++
		}
	}
	return maximum, nil
}

// ValidateLabelComposerScripts compiles configured label and rule scripts
// without evaluating them against feature data. Call this before persisting
// settings so syntax errors are reported at the edit boundary.
func ValidateLabelComposerScripts(textScript, ruleScript string) error {
	textScript = strings.TrimSpace(textScript)
	ruleScript = strings.TrimSpace(ruleScript)
	if textScript == "" && ruleScript == "" {
		return nil
	}
	if textScript == "" {
		textScript = "return nil"
	}
	program, err := CompileLabelComposerProgram(textScript, ruleScript)
	if err != nil {
		return err
	}
	program.Close()
	return nil
}

// LabelComposerProgram evaluates the optional rule and text script in one Lua
// call per feature. It reuses the sandbox state and read-only feature view.
type LabelComposerProgram struct {
	mu          sync.Mutex
	state       *lua.LState
	fn          *lua.LFunction
	feature     *lua.LTable
	featureData *lua.LTable
	featureKeys []string
}

func CompileLabelComposerProgram(textScript, ruleScript string) (*LabelComposerProgram, error) {
	if len(textScript) == 0 || len(textScript) > maxLabelScriptBytes || len(ruleScript) > maxLabelScriptBytes || len(textScript)+len(ruleScript) > maxLabelScriptBytes {
		return nil, fmt.Errorf("combined label scripts must be non-empty and no larger than %d bytes", maxLabelScriptBytes)
	}
	source := "local __text = function()\n" + textScript + "\nend\n"
	if strings.TrimSpace(ruleScript) != "" {
		source += "local __rule = function()\n" + ruleScript + "\nend\n"
		source += "return function() local matched = __rule(); if type(matched) ~= \"boolean\" then error(\"label rule must return boolean\") end; if not matched then return false, nil end; return true, __text() end"
	} else {
		source += "return function() return true, __text() end"
	}
	state := newLabelState()
	chunk, err := state.LoadString(source)
	if err != nil {
		state.Close()
		return nil, fmt.Errorf("compile label composer: %w", err)
	}
	state.Push(chunk)
	if err := state.PCall(0, 1, nil); err != nil {
		state.Close()
		return nil, fmt.Errorf("initialize label composer: %w", err)
	}
	fn, ok := state.Get(-1).(*lua.LFunction)
	state.Pop(1)
	if !ok {
		state.Close()
		return nil, errors.New("label composer did not compile to a function")
	}
	feature, featureData := newReadOnlyFeature(state)
	return &LabelComposerProgram{state: state, fn: fn, feature: feature, featureData: featureData}, nil
}

func (p *LabelComposerProgram) Evaluate(ctx context.Context, properties map[string]any) (string, bool, error) {
	if p == nil {
		return "", false, errors.New("label composer is closed")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == nil {
		return "", false, errors.New("label composer is closed")
	}
	evalContext, cancel := context.WithTimeout(ctx, maxLabelEvaluationDuration)
	defer cancel()
	setLuaContext(p.state, evalContext)
	if err := updateLabelFeature(evalContext, p.featureData, &p.featureKeys, properties); err != nil {
		p.state.RemoveContext()
		return "", false, err
	}
	p.state.SetGlobal("feature", p.feature)
	p.state.Push(p.fn)
	if err := p.state.PCall(0, 2, nil); err != nil {
		p.state.RemoveContext()
		return "", false, fmt.Errorf("evaluate label composer: %w", err)
	}
	matchedValue := p.state.Get(-2)
	textValue := p.state.Get(-1)
	p.state.Pop(2)
	p.state.RemoveContext()
	if matchedValue.Type() != lua.LTBool {
		return "", false, fmt.Errorf("label rule must return boolean, got %s", matchedValue.Type())
	}
	if !lua.LVAsBool(matchedValue) {
		return "", false, nil
	}
	switch textValue.Type() {
	case lua.LTString:
		text := strings.TrimSpace(textValue.String())
		if len(text) > maxLabelScriptBytes {
			return "", false, fmt.Errorf("label script result exceeds %d bytes", maxLabelScriptBytes)
		}
		return text, true, nil
	case lua.LTNumber:
		return textValue.String(), true, nil
	case lua.LTNil:
		return "", true, nil
	default:
		return "", false, fmt.Errorf("label script must return string, number, or nil, got %s", textValue.Type())
	}
}

func (p *LabelComposerProgram) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != nil {
		p.state.Close()
		p.state = nil
		p.fn = nil
		p.feature = nil
		p.featureData = nil
		p.featureKeys = nil
	}
}

func (p *LabelProgram) Evaluate(ctx context.Context, properties map[string]any) (lua.LValue, error) {
	if p == nil {
		return nil, errors.New("label program is closed")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == nil {
		return nil, errors.New("label program is closed")
	}
	evalContext, cancel := context.WithTimeout(ctx, maxLabelEvaluationDuration)
	defer cancel()
	setLuaContext(p.state, evalContext)
	if err := updateLabelFeature(evalContext, p.featureData, &p.featureKeys, properties); err != nil {
		p.state.RemoveContext()
		return nil, err
	}
	p.state.SetGlobal("feature", p.feature)
	p.state.Push(p.fn)
	err := p.state.PCall(0, 1, nil)
	if err != nil {
		p.state.RemoveContext()
		return nil, fmt.Errorf("evaluate label script: %w", err)
	}
	result := p.state.Get(-1)
	p.state.Pop(1)
	p.state.RemoveContext()
	return result, nil
}

// setLuaContext keeps GopherLua's per-instruction cancellation checks only
// when the supplied context can actually be canceled. Background and
// value-only contexts have a nil Done channel, so the standard VM loop is
// equivalent and avoids a select on every Lua instruction.
func setLuaContext(state *lua.LState, ctx context.Context) {
	if ctx.Done() == nil {
		state.RemoveContext()
		return
	}
	state.SetContext(ctx)
}

func updateLabelFeature(ctx context.Context, featureData *lua.LTable, featureKeys *[]string, properties map[string]any) error {
	if len(properties) > maxLabelFeatureProperties {
		return fmt.Errorf("feature has %d properties; label sandbox limit is %d", len(properties), maxLabelFeatureProperties)
	}
	keys := *featureKeys
	work := 0
	for _, key := range keys {
		work++
		if work&255 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if _, exists := properties[key]; !exists {
			featureData.RawSetString(key, lua.LNil)
		}
	}
	keys = keys[:0]
	for key, value := range properties {
		work++
		if work&255 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		featureData.RawSetString(key, labelLuaValue(value))
		keys = append(keys, key)
	}
	*featureKeys = keys
	return ctx.Err()
}

func newReadOnlyFeature(state *lua.LState) (*lua.LTable, *lua.LTable) {
	proxy := state.NewTable()
	data := state.NewTable()
	metatable := state.NewTable()
	metatable.RawSetString("__index", data)
	metatable.RawSetString("__newindex", state.NewFunction(func(state *lua.LState) int {
		state.RaiseError("feature table is read-only")
		return 0
	}))
	metatable.RawSetString("__metatable", lua.LString("protected feature table"))
	state.SetMetatable(proxy, metatable)
	guardFeatureTableMutators(state, proxy)
	return proxy, data
}

// table.insert/remove/sort mutate LTable values directly and bypass Lua's
// __newindex metamethod. Guard just the exposed feature proxy while preserving
// those standard functions for scripts' ordinary local tables.
func guardFeatureTableMutators(state *lua.LState, feature *lua.LTable) {
	tableLibrary, ok := state.GetGlobal("table").(*lua.LTable)
	if !ok {
		return
	}
	for _, name := range []string{"insert", "remove", "sort"} {
		original, ok := tableLibrary.RawGetString(name).(*lua.LFunction)
		if !ok {
			continue
		}
		tableLibrary.RawSetString(name, state.NewClosure(guardTableMutation, feature, original))
	}
}

func guardTableMutation(state *lua.LState) int {
	if state.Get(1) == state.Get(lua.UpvalueIndex(1)) {
		state.RaiseError("feature table is read-only")
		return 0
	}
	argumentCount := state.GetTop()
	arguments := make([]lua.LValue, argumentCount)
	for index := range arguments {
		arguments[index] = state.Get(index + 1)
	}
	stackTop := state.GetTop()
	original := state.Get(lua.UpvalueIndex(2)).(*lua.LFunction)
	if err := state.CallByParam(lua.P{Fn: original, NRet: lua.MultRet, Protect: true}, arguments...); err != nil {
		state.RaiseError("table operation: %v", err)
		return 0
	}
	return state.GetTop() - stackTop
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
		p.feature = nil
		p.featureData = nil
		p.featureKeys = nil
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
