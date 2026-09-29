//go:build !native

package main

import (
	"context"
	"fmt"
)

func runMerge(context.Context, mergeOptions) error {
	return fmt.Errorf("merge requires a native build: use scripts/build.sh native")
}
