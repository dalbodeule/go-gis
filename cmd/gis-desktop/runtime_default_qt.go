//go:build qt && !native

package main

func loadRuntime(_ []string) *demoRuntime {
	return loadDemoChunk()
}

func startInitialDataLoad(_ *demoRuntime, _ []string) {}

func (r *demoRuntime) startDataLoad(_, _, _, _, _ string, _ bool) {}
