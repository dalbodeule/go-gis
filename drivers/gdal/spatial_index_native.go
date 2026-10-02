//go:build native

package gdal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/airbusgeo/godal"
)

// HasShapefileSpatialIndex reports whether GDAL's QIX sidecar is present.
func HasShapefileSpatialIndex(source string) bool {
	if !strings.EqualFold(filepath.Ext(source), ".shp") {
		return false
	}
	_, err := findShapefileSidecar(source, ".qix")
	return err == nil
}

// CreateShapefileSpatialIndex creates a QIX sidecar beside source. It changes
// no SHP, SHX, DBF, or projection data, but requires write permission to the
// source directory.
func CreateShapefileSpatialIndex(ctx context.Context, source, layerName string) error {
	if !strings.EqualFold(filepath.Ext(source), ".shp") {
		return fmt.Errorf("spatial index creation requires a .shp source")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if HasShapefileSpatialIndex(source) {
		return nil
	}
	registerDrivers()
	dataset, err := godal.Open(source, godal.VectorOnly(), godal.Update())
	if err != nil {
		return fmt.Errorf("open shapefile for QIX creation: %w", err)
	}
	if layerName == "" {
		layers := dataset.Layers()
		if len(layers) == 0 {
			_ = dataset.Close()
			return fmt.Errorf("shapefile has no vector layer")
		}
		layerName = layers[0].Name()
	}
	result, sqlErr := dataset.ExecuteSQL("CREATE SPATIAL INDEX ON " + quoteSQLIdentifier(layerName))
	if result != nil {
		if closeErr := result.Close(); sqlErr == nil {
			sqlErr = closeErr
		}
	}
	closeErr := dataset.Close()
	if sqlErr != nil || closeErr != nil {
		return errors.Join(sqlErr, closeErr)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !HasShapefileSpatialIndex(source) {
		return fmt.Errorf("GDAL reported success but did not create a .qix sidecar")
	}
	return nil
}

// PrepareIndexedShapefileCache copies the shapefile components into a
// fingerprinted temporary cache and creates the QIX there. The returned path
// is safe to open through GDAL without modifying the original source.
func PrepareIndexedShapefileCache(ctx context.Context, source, layerName string) (string, bool, error) {
	if !strings.EqualFold(filepath.Ext(source), ".shp") {
		return "", false, fmt.Errorf("temporary spatial index cache requires a .shp source")
	}
	absoluteSource, err := filepath.Abs(source)
	if err != nil {
		return "", false, fmt.Errorf("resolve shapefile path: %w", err)
	}
	source = absoluteSource
	if HasShapefileSpatialIndex(source) {
		return source, false, nil
	}
	files, signature, err := shapefileCacheSignature(source)
	if err != nil {
		return "", false, err
	}
	tempRoot := filepath.Join(os.TempDir(), "gogis-qix-cache")
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		return "", false, fmt.Errorf("create temporary spatial-index cache: %w", err)
	}
	targetDir := filepath.Join(tempRoot, signature)
	targetSource := filepath.Join(targetDir, filepath.Base(source))
	if HasShapefileSpatialIndex(targetSource) {
		return targetSource, false, nil
	}
	if _, err := os.Stat(targetDir); err == nil {
		if err := os.RemoveAll(targetDir); err != nil {
			return "", false, fmt.Errorf("remove incomplete temporary QIX cache: %w", err)
		}
	}

	stageDir, err := os.MkdirTemp(tempRoot, ".building-")
	if err != nil {
		return "", false, fmt.Errorf("create temporary spatial-index staging directory: %w", err)
	}
	defer os.RemoveAll(stageDir)
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return "", false, err
		}
		if err := copyShapefileComponent(file, filepath.Join(stageDir, filepath.Base(file))); err != nil {
			return "", false, err
		}
	}
	stagedSource := filepath.Join(stageDir, filepath.Base(source))
	if err := CreateShapefileSpatialIndex(ctx, stagedSource, layerName); err != nil {
		return "", false, fmt.Errorf("create temporary QIX: %w", err)
	}
	_, currentSignature, err := shapefileCacheSignature(source)
	if err != nil {
		return "", false, err
	}
	if currentSignature != signature {
		return "", false, fmt.Errorf("source shapefile changed while its index cache was being prepared")
	}
	if err := os.Rename(stageDir, targetDir); err != nil {
		if HasShapefileSpatialIndex(targetSource) {
			return targetSource, false, nil
		}
		return "", false, fmt.Errorf("publish temporary QIX cache: %w", err)
	}
	return targetSource, true, nil
}

func shapefileCacheSignature(source string) ([]string, string, error) {
	if !strings.EqualFold(filepath.Ext(source), ".shp") {
		return nil, "", fmt.Errorf("not a shapefile: %s", source)
	}
	entries, err := os.ReadDir(filepath.Dir(source))
	if err != nil {
		return nil, "", fmt.Errorf("read shapefile directory: %w", err)
	}
	wantedStem := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	allowed := map[string]bool{
		".shp": true, ".shx": true, ".dbf": true, ".prj": true,
		".cpg": true, ".qpj": true, ".sbn": true, ".sbx": true,
	}
	files := make([]string, 0, len(allowed))
	for _, entry := range entries {
		name := entry.Name()
		ext := filepath.Ext(name)
		stem := strings.TrimSuffix(name, ext)
		if !strings.EqualFold(stem, wantedStem) || !allowed[strings.ToLower(ext)] {
			continue
		}
		info, statErr := entry.Info()
		if statErr != nil {
			return nil, "", fmt.Errorf("stat shapefile component %q: %w", name, statErr)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		files = append(files, filepath.Join(filepath.Dir(source), name))
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, "", fmt.Errorf("no shapefile components found for %q", source)
	}
	foundSource := false
	hash := sha256.New()
	_, _ = io.WriteString(hash, filepath.Clean(source)+"\x00")
	for _, file := range files {
		info, statErr := os.Stat(file)
		if statErr != nil {
			return nil, "", fmt.Errorf("stat shapefile component %q: %w", file, statErr)
		}
		if strings.EqualFold(file, source) {
			foundSource = true
		}
		_, _ = fmt.Fprintf(hash, "%s\x00%d\x00%d\x00", filepath.Base(file), info.Size(), info.ModTime().UnixNano())
	}
	if !foundSource {
		return nil, "", fmt.Errorf("shapefile geometry file %q is missing or not a regular file", source)
	}
	return files, hex.EncodeToString(hash.Sum(nil)), nil
}

func findShapefileSidecar(source, extension string) (string, error) {
	entries, err := os.ReadDir(filepath.Dir(source))
	if err != nil {
		return "", err
	}
	wantedStem := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	for _, entry := range entries {
		name := entry.Name()
		if strings.EqualFold(filepath.Ext(name), extension) &&
			strings.EqualFold(strings.TrimSuffix(name, filepath.Ext(name)), wantedStem) &&
			entry.Type().IsRegular() {
			return filepath.Join(filepath.Dir(source), name), nil
		}
	}
	return "", os.ErrNotExist
}

func copyShapefileComponent(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open shapefile component %q: %w", source, err)
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create cached shapefile component %q: %w", destination, err)
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil {
		return errors.Join(copyErr, closeErr)
	}
	return nil
}
