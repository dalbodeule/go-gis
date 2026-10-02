//go:build qt && !windows

package main

import (
	"fmt"
	"os"
	"syscall"
)

func startDesktopOutputCapture() error {
	stdoutMirrorFD, err := syscall.Dup(1)
	if err != nil {
		return fmt.Errorf("duplicate stdout: %w", err)
	}
	stderrMirrorFD, err := syscall.Dup(2)
	if err != nil {
		_ = syscall.Close(stdoutMirrorFD)
		return fmt.Errorf("duplicate stderr: %w", err)
	}
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		closeOutputMirrors(stdoutMirrorFD, stderrMirrorFD)
		return fmt.Errorf("create stdout log pipe: %w", err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		closeOutputPipes(stdoutReader, stdoutWriter)
		closeOutputMirrors(stdoutMirrorFD, stderrMirrorFD)
		return fmt.Errorf("create stderr log pipe: %w", err)
	}
	if err := syscall.Dup2(int(stdoutWriter.Fd()), 1); err != nil {
		closeOutputPipes(stdoutReader, stdoutWriter, stderrReader, stderrWriter)
		closeOutputMirrors(stdoutMirrorFD, stderrMirrorFD)
		return fmt.Errorf("redirect stdout: %w", err)
	}
	if err := syscall.Dup2(int(stderrWriter.Fd()), 2); err != nil {
		_ = syscall.Dup2(stdoutMirrorFD, 1)
		closeOutputPipes(stdoutReader, stdoutWriter, stderrReader, stderrWriter)
		closeOutputMirrors(stdoutMirrorFD, stderrMirrorFD)
		return fmt.Errorf("redirect stderr: %w", err)
	}
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()
	startOutputPipe("stdout", stdoutReader, outputMirrorFile(stdoutMirrorFD, "stdout-original"))
	startOutputPipe("stderr", stderrReader, outputMirrorFile(stderrMirrorFD, "stderr-original"))
	return nil
}

func closeOutputMirrors(descriptors ...int) {
	for _, descriptor := range descriptors {
		if descriptor >= 0 {
			_ = syscall.Close(descriptor)
		}
	}
}

func outputMirrorFile(descriptor int, name string) *os.File {
	if descriptor < 0 {
		return nil
	}
	return os.NewFile(uintptr(descriptor), name)
}
