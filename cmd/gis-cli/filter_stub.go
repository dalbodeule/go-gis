//go:build !native

package main

import (
	"context"
	"fmt"
)

func runFilter(context.Context, filterOptions) error {
	return fmt.Errorf("filter requires a native build: use scripts/build.sh native")
}
