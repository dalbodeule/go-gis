//go:build qt

package main

import (
	"bufio"
	"io"
	"os"

	"gogis/ui/qt/native"
)

const capturedOutputLineLimit = 16 << 10

func closeOutputPipes(files ...*os.File) {
	for _, file := range files {
		if file != nil {
			_ = file.Close()
		}
	}
}

func startOutputPipe(stream string, reader, mirror *os.File) {
	go func() {
		defer reader.Close()
		if mirror != nil {
			defer mirror.Close()
		}
		buffered := bufio.NewReaderSize(reader, 4096)
		line := make([]byte, 0, capturedOutputLineLimit)
		truncated := false
		publish := func() {
			if truncated {
				line = append(line, []byte(" … [truncated]")...)
			}
			native.RecordDiagnostic(stream, string(line))
			line = line[:0]
			truncated = false
		}
		for {
			fragment, err := buffered.ReadSlice('\n')
			if mirror != nil && len(fragment) > 0 {
				_, _ = mirror.Write(fragment)
			}
			content := fragment
			terminated := len(content) > 0 && content[len(content)-1] == '\n'
			if terminated {
				content = content[:len(content)-1]
			}
			remaining := capturedOutputLineLimit - len(line)
			if remaining > 0 {
				take := len(content)
				if take > remaining {
					take = remaining
					truncated = true
				}
				line = append(line, content[:take]...)
			} else if len(content) > 0 {
				truncated = true
			}
			if terminated {
				publish()
			}
			if err == nil || err == bufio.ErrBufferFull {
				continue
			}
			if err != io.EOF {
				native.RecordDiagnostic("capture", "stdout/stderr capture stopped: "+err.Error())
			}
			if len(line) > 0 || truncated {
				publish()
			}
			return
		}
	}()
}
