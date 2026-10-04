//go:build qt && windows

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func startDesktopOutputCapture() error {
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("create stdout log pipe: %w", err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		closeOutputPipes(stdoutReader, stdoutWriter)
		return fmt.Errorf("create stderr log pipe: %w", err)
	}
	originalStdout, originalStderr := os.Stdout, os.Stderr
	originalStdoutHandle, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err != nil {
		originalStdoutHandle = 0
		originalStdout = nil
	}
	if err := windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, windows.Handle(stdoutWriter.Fd())); err != nil {
		closeOutputPipes(stdoutReader, stdoutWriter, stderrReader, stderrWriter)
		return fmt.Errorf("redirect stdout handle: %w", err)
	}
	if err := windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(stderrWriter.Fd())); err != nil {
		_ = windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, originalStdoutHandle)
		closeOutputPipes(stdoutReader, stdoutWriter, stderrReader, stderrWriter)
		return fmt.Errorf("redirect stderr handle: %w", err)
	}
	// Go writes use the os.File values captured during process startup, so
	// replace them as well as the Win32 standard handles used by native code.
	os.Stdout, os.Stderr = stdoutWriter, stderrWriter
	startOutputPipe("stdout", stdoutReader, originalStdout)
	startOutputPipe("stderr", stderrReader, originalStderr)
	return nil
}
