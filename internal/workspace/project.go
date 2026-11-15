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
	pathpkg "path"
	"path/filepath"
	"runtime"
	"strings"

	"gogis/internal/core"
	"golang.org/x/text/unicode/norm"
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
	GeometryType    string             `json:"geometryType,omitempty"`
	Visible         bool               `json:"visible"`
	Style           core.LayerStyle    `json:"style"`
	Labels          core.LabelSettings `json:"labels"`
	DisplayRule     string             `json:"displayRule,omitempty"`
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
			GeometryType: layer.GeometryType,
			Style:        layer.Style, Labels: layer.Labels, DisplayRule: layer.DisplayRule,
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
			GeometryType: layer.GeometryType, GeometryLocked: true, Visible: layer.Visible, Style: layer.Style, Labels: layer.Labels, DisplayRule: layer.DisplayRule,
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
		if isSourceURI(source) {
			continue
		}
		if isForeignAbsolutePath(source, strings.ReplaceAll(source, "\\", "/")) {
			continue
		}
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
	var nearby map[string][]string
	var nearbyComplete bool
	nearbyScanned := false
	for index := range doc.Layers {
		source := doc.Layers[index].SourcePath
		if isSourceURI(source) {
			continue
		}
		// A Windows absolute path is not meaningful to filepath.IsAbs on
		// POSIX. Preserve it for display/relinking instead of incorrectly
		// treating it as relative to the workspace directory.
		portable := strings.ReplaceAll(source, "\\", "/")
		if isForeignAbsolutePath(source, portable) {
			doc.Layers[index].SourcePath = source
		} else if local := filepath.FromSlash(portable); filepath.IsAbs(local) {
			doc.Layers[index].SourcePath = filepath.Clean(local)
		} else {
			doc.Layers[index].SourcePath = filepath.Clean(filepath.Join(workspaceDirectory, local))
		}
		if info, statErr := os.Stat(doc.Layers[index].SourcePath); statErr == nil && info.Mode().IsRegular() {
			continue
		} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if !nearbyScanned {
			nearby, nearbyComplete = nearbyWorkspaceSources(workspaceDirectory)
			nearbyScanned = true
		}
		if nearbyComplete {
			if candidates := nearby[portableSourceKey(pathpkg.Base(portable))]; len(candidates) == 1 {
				doc.Layers[index].SourcePath = candidates[0]
			}
		}
	}
	if _, err := doc.Project(); err != nil {
		return Document{}, err
	}
	return doc, nil
}

func isSourceURI(source string) bool {
	return strings.Contains(source, "://") && !isWindowsAbsolutePath(source)
}

func isForeignAbsolutePath(source, portable string) bool {
	return runtime.GOOS != "windows" && isWindowsAbsolutePath(source) ||
		runtime.GOOS == "windows" && strings.HasPrefix(portable, "/") && !strings.HasPrefix(portable, "//")
}

func portableSourceKey(name string) string {
	return strings.ToLower(norm.NFC.String(name))
}

// Search only the workspace folder and two nested levels. A unique filename
// can recover a moved project tree; duplicate names stay unavailable so the
// application never silently chooses a different dataset. A capped scan
// fails closed if a very large folder cannot be examined completely.
func nearbyWorkspaceSources(root string) (map[string][]string, bool) {
	const maxEntries = 20_000
	const maxDepth = 2
	type directory struct {
		path  string
		depth int
	}
	queue := []directory{{path: root}}
	result := make(map[string][]string)
	seenEntries := 0
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		folder, err := os.Open(current.path)
		if err != nil {
			return nil, false
		}
		for {
			entries, readErr := folder.ReadDir(256)
			for _, entry := range entries {
				seenEntries++
				if seenEntries > maxEntries {
					_ = folder.Close()
					return nil, false
				}
				if strings.HasPrefix(entry.Name(), ".") || entry.Type()&os.ModeSymlink != 0 {
					continue
				}
				candidate := filepath.Join(current.path, entry.Name())
				if entry.IsDir() {
					if current.depth < maxDepth {
						queue = append(queue, directory{path: candidate, depth: current.depth + 1})
					}
					continue
				}
				if entry.Type().IsRegular() {
					key := portableSourceKey(entry.Name())
					result[key] = append(result[key], candidate)
				}
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				_ = folder.Close()
				return nil, false
			}
		}
		if err := folder.Close(); err != nil {
			return nil, false
		}
	}
	return result, true
}

func isWindowsAbsolutePath(path string) bool {
	if len(path) >= 3 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) &&
		path[1] == ':' && (path[2] == '/' || path[2] == '\\') {
		return true
	}
	return strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//")
}
