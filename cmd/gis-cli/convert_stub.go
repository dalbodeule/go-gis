//go:build !native

package main

import (
	"context"
	"fmt"
)

func runConvert(_ context.Context, _ convertOptions) error {
	return fmt.Errorf("convert requires a native build: use scripts/build.sh native")
}
