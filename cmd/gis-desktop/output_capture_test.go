//go:build qt

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"gogis/ui/qt/native"
)

func TestDesktopOutputCapturePreservesConsoleAndCapturesStandardStreams(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=^TestDesktopOutputCaptureSubprocess$")
	command.Env = append(os.Environ(), "GOGIS_TEST_OUTPUT_CAPTURE_SUBPROCESS=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("capture subprocess failed: %v\n%s", err, output)
	}
	for _, expected := range []string{"capture-subprocess-stdout", "capture-subprocess-stderr", "GoGIS: Render incomplete; capture-subprocess-error", "GoGIS: No visible layers"} {
		if !strings.Contains(string(output), expected) {
			t.Fatalf("mirrored console output %q missing from %q", expected, output)
		}
	}
}

func TestDesktopOutputCaptureSubprocess(t *testing.T) {
	if os.Getenv("GOGIS_TEST_OUTPUT_CAPTURE_SUBPROCESS") != "1" {
		return
	}
	if err := startDesktopOutputCapture(); err != nil {
		t.Fatal(err)
	}
	_, _ = fmt.Fprintln(os.Stdout, "capture-subprocess-stdout")
	_, _ = fmt.Fprintln(os.Stderr, "capture-subprocess-stderr")
	native.SetRenderStatus("Render incomplete; capture-subprocess-error")
	native.SetRenderStatus("No visible layers")
	deadline := time.Now().Add(2 * time.Second)
	for {
		logs := native.DiagnosticLogJSON()
		if strings.Contains(logs, "capture-subprocess-stdout") && strings.Contains(logs, "capture-subprocess-stderr") &&
			strings.Contains(logs, "capture-subprocess-error") && strings.Contains(logs, "No visible layers") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("standard output not captured: %s", logs)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestStartOutputPipeCapturesBoundedLinesAndMirrorsBytes(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	mirror, err := os.CreateTemp(t.TempDir(), "output-mirror-")
	if err != nil {
		t.Fatal(err)
	}
	marker := fmt.Sprintf("capture-test-%d", time.Now().UnixNano())
	startOutputPipe("stderr-test", reader, mirror)
	largeLine := strings.Repeat("x", capturedOutputLineLimit*3)
	want := marker + "\n" + largeLine + "\npartial-output"
	if _, err := writer.WriteString(want); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		logs := native.DiagnosticLogJSON()
		if strings.Contains(logs, marker) && strings.Contains(logs, "partial-output") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("captured output did not appear in the diagnostic ring")
		}
		time.Sleep(time.Millisecond)
	}
	for {
		info, err := os.Stat(mirror.Name())
		if err == nil && info.Size() == int64(len(want)) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("mirrored output size = %v, want %d", info, len(want))
		}
		time.Sleep(time.Millisecond)
	}
	mirrored, err := os.ReadFile(mirror.Name())
	if err != nil {
		t.Fatal(err)
	}
	if string(mirrored) != want {
		t.Fatalf("mirrored output differs: got %d bytes, want %d", len(mirrored), len(want))
	}
	log := native.DiagnosticLogJSON()
	if !strings.Contains(log, "[truncated]") || !strings.Contains(log, "partial-output") {
		t.Fatalf("long or unterminated output was not captured safely: %s", log)
	}
}
