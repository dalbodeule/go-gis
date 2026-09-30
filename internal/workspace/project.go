// Package workspace persists editable desktop project state without embedding
// source datasets. Layer paths continue to refer to the original datasets.
package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"gogis/internal/core"
)

const CurrentVersion = 1

type Layer struct {
	Name            string             `json:"name"`
	DisplayName     string             `json:"displayName,omitempty"`
	SourcePath      string             `json:"sourcePath"`
	SourceLayerName string             `json:"sourceLayerName,omitempty"`
	SourceEncoding  string             `json:"sourceEncoding,omitempty"`
	SourceCRS       string             `json:"sourceCrs,omitempty"`
	CRS             string             `json:"crs,omitempty"`
	Visible         bool               `json:"visible"`
	Style           core.LayerStyle    `json:"style"`
	Labels          core.LabelSettings `json:"labels"`
}

type Document struct {
	Version int        `json:"version"`
	Name    string     `json:"name"`
	CRS     string     `json:"crs,omitempty"`
	View    *ViewState `json:"view,omitempty"`
	Layers  []Layer    `json:"layers"`
}

// ViewState preserves the map framing independently of the window's pixel size.
type ViewState struct {
	CenterX     float64 `json:"centerX"`
	CenterY     float64 `json:"centerY"`
	Zoom        float64 `json:"zoom"`
	ActiveLayer string  `json:"activeLayer,omitempty"`
}

func FromProject(project core.Project) Document {
	doc := Document{Version: CurrentVersion, Name: project.Name, CRS: project.CRS.AuthorityCode, Layers: make([]Layer, len(project.Layers))}
	for index, layer := range project.Layers {
		doc.Layers[index] = Layer{
			Name: layer.Name, DisplayName: layer.DisplayName, SourcePath: layer.SourcePath, SourceLayerName: layer.SourceLayerName,
			SourceEncoding: layer.SourceEncoding, SourceCRS: layer.SourceCRS, CRS: layer.CRS.AuthorityCode, Visible: layer.Visible,
			Style: layer.Style, Labels: layer.Labels,
		}
	}
	return doc
}

func (d Document) Project() (core.Project, error) {
	if d.Version != CurrentVersion {
		return core.Project{}, fmt.Errorf("unsupported workspace version %d", d.Version)
	}
	if d.View != nil && (math.IsNaN(d.View.CenterX) || math.IsInf(d.View.CenterX, 0) ||
		math.IsNaN(d.View.CenterY) || math.IsInf(d.View.CenterY, 0) ||
		math.IsNaN(d.View.Zoom) || math.IsInf(d.View.Zoom, 0) || d.View.Zoom <= 0) {
		return core.Project{}, errors.New("workspace view state contains invalid coordinates or zoom")
	}
	project := core.Project{Name: d.Name, CRS: core.CRS{AuthorityCode: d.CRS}, Layers: make([]core.Layer, len(d.Layers))}
	for index, layer := range d.Layers {
		if layer.Name == "" || layer.SourcePath == "" {
			return core.Project{}, fmt.Errorf("workspace layer %d is missing its name or source path", index)
		}
		if err := layer.Style.Validate(); err != nil {
			return core.Project{}, fmt.Errorf("workspace layer %q: %w", layer.Name, err)
		}
		if err := layer.Labels.Validate(); err != nil {
			return core.Project{}, fmt.Errorf("workspace layer %q: %w", layer.Name, err)
		}
		project.Layers[index] = core.Layer{
			Name: layer.Name, DisplayName: layer.DisplayName, SourcePath: layer.SourcePath, SourceLayerName: layer.SourceLayerName,
			SourceEncoding: layer.SourceEncoding, SourceCRS: layer.SourceCRS, CRS: core.CRS{AuthorityCode: layer.CRS},
			Visible: layer.Visible, Style: layer.Style, Labels: layer.Labels,
		}
	}
	return project, nil
}

func Save(path string, doc Document) error {
	if path == "" {
		return errors.New("workspace path is empty")
	}
	if _, err := doc.Project(); err != nil {
		return err
	}
	workspaceDirectory, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return err
	}
	for index := range doc.Layers {
		source := doc.Layers[index].SourcePath
		if filepath.IsAbs(source) {
			if relative, relErr := filepath.Rel(workspaceDirectory, source); relErr == nil {
				doc.Layers[index].SourcePath = filepath.ToSlash(relative)
			}
		} else if !isWindowsAbsolutePath(source) {
			source = strings.ReplaceAll(source, "\\", "/")
			doc.Layers[index].SourcePath = filepath.ToSlash(filepath.Clean(filepath.FromSlash(source)))
		}
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".gogis-workspace-*.tmp")
	if err != nil {
		return fmt.Errorf("create workspace temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return replaceWorkspaceFile(temporaryPath, path)
}

func replaceWorkspaceFile(temporaryPath, destination string) error {
	if _, err := os.Stat(destination); os.IsNotExist(err) {
		if err := os.Rename(temporaryPath, destination); err != nil {
			return fmt.Errorf("install workspace file: %w", err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect existing workspace: %w", err)
	}
	directory := filepath.Dir(destination)
	backup, err := os.CreateTemp(directory, ".gogis-workspace-backup-*.tmp")
	if err != nil {
		return fmt.Errorf("reserve workspace backup: %w", err)
	}
	backupPath := backup.Name()
	if err := backup.Close(); err != nil {
		_ = os.Remove(backupPath)
		return fmt.Errorf("close workspace backup: %w", err)
	}
	if err := os.Remove(backupPath); err != nil {
		return fmt.Errorf("prepare workspace backup: %w", err)
	}
	if err := os.Rename(destination, backupPath); err != nil {
		return fmt.Errorf("preserve previous workspace: %w", err)
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		if restoreErr := os.Rename(backupPath, destination); restoreErr != nil {
			return fmt.Errorf("replace workspace: %w; previous file remains at %q: %v", err, backupPath, restoreErr)
		}
		return fmt.Errorf("replace workspace: %w", err)
	}
	if err := os.Remove(backupPath); err != nil {
		return fmt.Errorf("workspace saved but old version remains at %q: %w", backupPath, err)
	}
	return nil
}

func Load(path string) (Document, error) {
	file, err := os.Open(path)
	if err != nil {
		return Document{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 16<<20))
	decoder.DisallowUnknownFields()
	var doc Document
	if err := decoder.Decode(&doc); err != nil {
		return Document{}, fmt.Errorf("decode workspace: %w", err)
	}
	workspaceDirectory, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return Document{}, err
	}
	for index := range doc.Layers {
		source := doc.Layers[index].SourcePath
		// A Windows absolute path is not meaningful to filepath.IsAbs on
		// POSIX. Preserve it for display/relinking instead of incorrectly
		// treating it as relative to the workspace directory.
		if isWindowsAbsolutePath(source) && !filepath.IsAbs(source) {
			continue
		}
		source = strings.ReplaceAll(source, "\\", "/")
		source = filepath.FromSlash(source)
		if filepath.IsAbs(source) {
			doc.Layers[index].SourcePath = filepath.Clean(source)
		} else {
			doc.Layers[index].SourcePath = filepath.Clean(filepath.Join(workspaceDirectory, source))
		}
	}
	if _, err := doc.Project(); err != nil {
		return Document{}, err
	}
	return doc, nil
}

func isWindowsAbsolutePath(path string) bool {
	if len(path) >= 3 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) &&
		path[1] == ':' && (path[2] == '/' || path[2] == '\\') {
		return true
	}
	return strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//")
}
