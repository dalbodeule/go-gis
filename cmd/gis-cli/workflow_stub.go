//go:build !native

package main

import (
	"context"
	"fmt"
)

func runWorkflow(context.Context, string, workflowOptions) error {
	return fmt.Errorf("workflow commands require a native build: use scripts/build.sh native")
}
