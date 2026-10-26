package scripting

import (
	"math"
	"strconv"
	"strings"

	"github.com/yuin/gopher-lua"
)

// installLuaHelpers exposes pure, bounded formatting helpers in both the
// per-feature label sandbox and the project scripting runtime.
func installLuaHelpers(state *lua.LState) *lua.LTable {
	return state.SetFuncs(state.NewTable(), map[string]lua.LGFunction{
		"format_number": luaFormatNumber,
		"coalesce":      luaCoalesce,
		"trim":          luaTrim,
		"contains":      luaContains,
	})
}

func luaFormatNumber(state *lua.LState) int {
	value := state.Get(1)
	if value == lua.LNil {
		state.Push(lua.LNil)
		return 1
	}
	var number float64
	switch value := value.(type) {
	case lua.LNumber:
		number = float64(value)
	case lua.LString:
		if len(value) > maxLuaGeneratedStringBytes {
			state.RaiseError("format_number input exceeds the %d-byte sandbox limit", maxLuaGeneratedStringBytes)
			return 0
		}
		parsed, err := strconv.ParseFloat(strings.TrimSpace(string(value)), 64)
		if err != nil {
			state.Push(lua.LNil)
			return 1
		}
		number = parsed
	default:
		state.RaiseError("format_number expects a number or numeric string")
		return 0
	}
	if math.IsNaN(number) || math.IsInf(number, 0) {
		state.Push(lua.LNil)
		return 1
	}
	decimals := 0
	if state.GetTop() >= 2 && state.Get(2) != lua.LNil {
		precision := float64(state.CheckNumber(2))
		if precision < 0 || precision > 6 || precision != math.Trunc(precision) {
			state.RaiseError("format_number decimal places must be an integer from 0 to 6")
			return 0
		}
		decimals = int(precision)
	}
	formatted := strconv.FormatFloat(number, 'f', decimals, 64)
	integerEnd := strings.IndexByte(formatted, '.')
	if integerEnd < 0 {
		integerEnd = len(formatted)
	}
	firstDigit := 0
	if strings.HasPrefix(formatted, "-") {
		firstDigit = 1
	}
	var grouped strings.Builder
	grouped.Grow(len(formatted) + (integerEnd-firstDigit-1)/3)
	grouped.WriteString(formatted[:firstDigit])
	for index := firstDigit; index < integerEnd; index++ {
		if index > firstDigit && (integerEnd-index)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteByte(formatted[index])
	}
	grouped.WriteString(formatted[integerEnd:])
	state.Push(lua.LString(grouped.String()))
	return 1
}

func luaCoalesce(state *lua.LState) int {
	for index := 1; index <= state.GetTop(); index++ {
		value := state.Get(index)
		if value != lua.LNil && value != lua.LString("") {
			state.Push(value)
			return 1
		}
	}
	state.Push(lua.LNil)
	return 1
}

func luaTrim(state *lua.LState) int {
	if state.Get(1) == lua.LNil {
		state.Push(lua.LNil)
		return 1
	}
	value := state.CheckString(1)
	if len(value) > maxLuaGeneratedStringBytes {
		state.RaiseError("trim input exceeds the %d-byte sandbox limit", maxLuaGeneratedStringBytes)
		return 0
	}
	state.Push(lua.LString(strings.TrimSpace(value)))
	return 1
}

func luaContains(state *lua.LState) int {
	if state.Get(1) == lua.LNil || state.Get(2) == lua.LNil {
		state.Push(lua.LFalse)
		return 1
	}
	value, fragment := state.CheckString(1), state.CheckString(2)
	if len(value) > maxLuaGeneratedStringBytes || len(fragment) > maxLuaGeneratedStringBytes {
		state.RaiseError("contains input exceeds the %d-byte sandbox limit", maxLuaGeneratedStringBytes)
		return 0
	}
	if state.OptBool(3, false) {
		value, fragment = strings.ToLower(value), strings.ToLower(fragment)
	}
	state.Push(lua.LBool(strings.Contains(value, fragment)))
	return 1
}
