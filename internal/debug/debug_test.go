package debug

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestLoggerIsOptIn(t *testing.T) {
	t.Setenv("GOGIS_DEBUG", "1")
	var output bytes.Buffer
	NewLogger(&output).Debug("driver opened", "source", "sample.gpkg")
	if !strings.Contains(output.String(), "driver opened") {
		t.Fatalf("debug output = %q", output.String())
	}

	if err := os.Unsetenv("GOGIS_DEBUG"); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	NewLogger(&output).Debug("hidden")
	if output.Len() != 0 {
		t.Fatalf("debug output should be empty, got %q", output.String())
	}
}
