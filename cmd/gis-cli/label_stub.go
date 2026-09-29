//go:build !native

package main

import (
	"context"
	"fmt"
)

func runLabel(context.Context, labelOptions) error {
	return fmt.Errorf("label requires a native build: use scripts/build.sh native")
}
