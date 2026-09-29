//go:build qt && !native

package main

func loadRuntime(_ []string) *demoRuntime {
	return loadDemoChunk()
}
