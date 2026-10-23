//go:build qt && native

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	geosdriver "gogis/drivers/geos"
	"gogis/ui/qt/native"
)

func applySpatialRequest(runtime *demoRuntime, payload string) error {
	var request spatialOperationRequest
	if err := json.Unmarshal([]byte(payload), &request); err != nil {
		return fmt.Errorf("invalid spatial request: %w", err)
	}
	request.Operation = strings.ToLower(strings.TrimSpace(request.Operation))
	request.Left = strings.TrimSpace(request.Left)
	request.Right = strings.TrimSpace(request.Right)
	request.Result = strings.TrimSpace(request.Result)
	if request.Left == "" || request.Result == "" {
		return fmt.Errorf("left and result layers are required")
	}
	if request.Operation != "buffer" && request.Right == "" {
		return fmt.Errorf("right layer is required for %s", request.Operation)
	}
	if request.Operation == "buffer" && request.Distance == 0 {
		return fmt.Errorf("buffer distance must not be zero")
	}

	runtime.mu.Lock()
	if runtime.service == nil || !runtime.dataMode {
		runtime.mu.Unlock()
		return fmt.Errorf("no editable project is loaded")
	}
	if runtime.readOnly {
		runtime.mu.Unlock()
		return fmt.Errorf("read-only projects cannot run spatial operations")
	}
	if runtime.operationCancel != nil {
		runtime.mu.Unlock()
		return fmt.Errorf("another spatial operation is already running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	runtime.operationCancel = cancel
	runtime.operationGeneration++
	operationGeneration := runtime.operationGeneration
	service := runtime.service
	saveDestination := runtime.saveDestination
	runtime.mu.Unlock()

	native.SetRenderStatus(fmt.Sprintf("Running %s", request.Operation))
	go func() {
		defer func() {
			runtime.mu.Lock()
			if runtime.operationGeneration == operationGeneration {
				runtime.operationCancel = nil
			}
			runtime.mu.Unlock()
		}()

		if err := service.ApplySpatialOperation(ctx, geosdriver.NewOperator(), request.Operation, request.Left, request.Right, request.Result, request.Distance); err != nil {
			if ctx.Err() != nil {
				native.SetRenderStatus("Spatial operation cancelled")
			} else {
				native.SetRenderStatus("Spatial operation failed: " + err.Error())
			}
			return
		}
		if err := ctx.Err(); err != nil {
			native.SetRenderStatus("Spatial operation cancelled")
			return
		}
		project := service.Project()
		next, err := buildDataRuntime(ctx, project.Layers, "", "", project.CRS.AuthorityCode, saveDestination, "", false, nil, nil)
		if err != nil {
			native.SetRenderStatus("Spatial result build failed: " + err.Error())
			return
		}
		runtime.mu.Lock()
		stale := runtime.service != service || runtime.operationGeneration != operationGeneration
		runtime.mu.Unlock()
		if stale {
			if next.closeAttributeSource != nil {
				next.closeAttributeSource()
			}
			return
		}
		runtime.replaceWith(next)
		native.SetRenderStatus("Spatial result added: " + request.Result)
	}()
	return nil
}
