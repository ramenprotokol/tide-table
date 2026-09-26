//go:build js && wasm

// Command tidewasm exposes the almanac to JavaScript as globalThis.tideTable:
//
//	tideTable.compute(expr, zones, mode, from, nowMs) → JSON string
//	tideTable.info → JSON string with the Go, tz database and cron versions
//	tideTable.zones → JSON array of zone names to suggest
package main

import (
	"runtime"
	"runtime/debug"
	"strings"
	"syscall/js"
	_ "time/tzdata" // embed the IANA tz database; the browser's zone data is never used

	"github.com/ramenprotokol/tide-table/internal/almanac"
)

func cronVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, d := range info.Deps {
			if d.Path == "github.com/robfig/cron/v3" {
				return d.Version
			}
		}
	}
	return "unknown"
}

func str(v js.Value) string {
	if v.Type() == js.TypeString {
		return v.String()
	}
	return ""
}

func compute(this js.Value, args []js.Value) any {
	for len(args) < 5 {
		args = append(args, js.Undefined())
	}
	req := almanac.Request{Expr: str(args[0]), Mode: str(args[2]), From: str(args[3])}
	if z := args[1]; z.Type() == js.TypeObject {
		n := z.Length()
		if n > almanac.MaxZones+1 { // enough to trigger the "at most" message
			n = almanac.MaxZones + 1
		}
		for i := 0; i < n; i++ {
			req.Zones = append(req.Zones, str(z.Index(i)))
		}
	}
	if t := args[4]; t.Type() == js.TypeNumber {
		req.Now = int64(t.Float())
	}
	return string(almanac.Compute(req).JSON())
}

func main() {
	api := js.Global().Get("Object").New()
	api.Set("compute", js.FuncOf(compute))
	api.Set("info", `{"go":"`+runtime.Version()+`","tzdata":"`+almanac.TZDataVersion+`","cron":"github.com/robfig/cron/v3 `+cronVersion()+`"}`)
	api.Set("zones", `["`+strings.Join(almanac.Zones(), `","`)+`"]`)
	js.Global().Set("tideTable", api)
	if ready := js.Global().Get("tideTableReady"); ready.Type() == js.TypeFunction {
		ready.Invoke()
	}
	select {}
}
