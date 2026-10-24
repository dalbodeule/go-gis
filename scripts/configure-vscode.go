//go:build ignore

// Run from the repository root with: go run -mod=readonly ./scripts/configure-vscode.go
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

func main() {
	if err := configure(); err != nil {
		fmt.Fprintln(os.Stderr, "VS Code setup:", err)
		os.Exit(1)
	}
}

func configure() error {
	qtpathsFlag := flag.String("qtpaths", "", "path to the Qt 6 qtpaths executable")
	qmllsFlag := flag.String("qmlls", "", "path to the matching QML language server")
	dryRun := flag.Bool("dry-run", false, "print settings without writing them")
	flag.Parse()

	root, err := os.Getwd()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return errors.New("run this command from the GoGIS repository root")
	}
	qtpaths, err := findTool(*qtpathsFlag, []string{"qtpaths6", "qtpaths"})
	if err != nil {
		return fmt.Errorf("find Qt 6 qtpaths: %w", err)
	}
	qmlDir, err := exec.Command(qtpaths, "--query", "QT_INSTALL_QML").Output()
	if err != nil {
		return fmt.Errorf("query Qt QML directory using %s: %w", qtpaths, err)
	}
	qmlPath := strings.TrimSpace(string(qmlDir))
	if !filepath.IsAbs(qmlPath) {
		return fmt.Errorf("Qt reported a non-absolute QML directory %q", qmlPath)
	}
	if _, err := os.Stat(filepath.Join(qmlPath, "QtQuick", "qmldir")); err != nil {
		return fmt.Errorf("QtQuick/qmldir is missing under %q: %w", qmlPath, err)
	}
	qmlls, err := findTool(*qmllsFlag, []string{"qmlls"})
	if err != nil {
		return fmt.Errorf("find QML language server: %w", err)
	}
	if err := checkQmllsVersion(qmlls); err != nil {
		return err
	}

	goEnv := map[string]string{"CGO_ENABLED": "1", "CGO_CXXFLAGS": "-std=c++17"}
	probeEnv := os.Environ()
	if runtime.GOOS == "windows" {
		binDir := filepath.Dir(qtpaths)
		prefix := filepath.Dir(binDir)
		compiler := map[string]string{"CC": "gcc.exe", "CXX": "g++.exe"}
		if runtime.GOARCH == "arm64" {
			compiler = map[string]string{"CC": "clang.exe", "CXX": "clang++.exe"}
		}
		for name, filename := range compiler {
			path := filepath.Join(binDir, filename)
			if _, err := os.Stat(path); err != nil {
				return fmt.Errorf("Windows native analysis requires MSYS2 %s alongside qtpaths: %w", filename, err)
			}
			goEnv[name] = path
		}
		goEnv["PKG_CONFIG_PATH"] = filepath.Join(prefix, "lib", "pkgconfig")
		goEnv["PROJ_DATA"] = filepath.Join(prefix, "share", "proj")
		goEnv["GDAL_DATA"] = filepath.Join(prefix, "share", "gdal")
		goEnv["PATH"] = binDir + string(os.PathListSeparator) + os.Getenv("PATH")
		msysBin := filepath.Join(filepath.Dir(prefix), "usr", "bin")
		if info, err := os.Stat(msysBin); err == nil && info.IsDir() {
			goEnv["PATH"] = binDir + string(os.PathListSeparator) + msysBin + string(os.PathListSeparator) + os.Getenv("PATH")
		}
		probeEnv = append(probeEnv, "PATH="+goEnv["PATH"], "PKG_CONFIG_PATH="+goEnv["PKG_CONFIG_PATH"])
	}
	if err := checkNativeModules(probeEnv, filepath.Dir(qtpaths)); err != nil {
		return err
	}

	settingsPath := filepath.Join(root, ".vscode", "settings.json")
	settings := map[string]any{}
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("existing %s is not plain JSON; preserve it and edit manually: %w", settingsPath, err)
		}
		if settings == nil {
			return fmt.Errorf("existing %s must contain a JSON object", settingsPath)
		}
		if existing, ok := settings["gopls"]; ok {
			if _, object := existing.(map[string]any); !object {
				return fmt.Errorf("existing %s gopls setting must be a JSON object", settingsPath)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	gopls, _ := settings["gopls"].(map[string]any)
	if gopls == nil {
		gopls = map[string]any{}
	}
	env, _ := gopls["env"].(map[string]any)
	if env == nil {
		env = map[string]any{}
	}
	for name, value := range goEnv {
		env[name] = value
	}
	gopls["buildFlags"] = []string{"-tags=native,qt"}
	gopls["env"] = env
	settings["gopls"] = gopls
	settings["qt-qml.qmlls.customExePath"] = qmlls
	settings["qt-qml.qmlls.additionalImportPaths"] = []string{qmlPath, "${workspaceFolder}/cmd/gis-desktop/qmltests"}
	settings["qt-qml.qmlls.useNoCMakeCalls"] = true
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if *dryRun {
		_, err = os.Stdout.Write(data)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(settingsPath), "settings-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), settingsPath); err != nil {
		return err
	}
	fmt.Println("Updated", settingsPath)
	fmt.Println("Reload the VS Code window, then restart the Go and QML language servers if needed.")
	return nil
}

func findTool(explicit string, names []string) (string, error) {
	if explicit != "" {
		path, err := filepath.Abs(explicit)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			return "", fmt.Errorf("%s is a directory, not an executable", path)
		}
		return path, nil
	}
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return filepath.Abs(path)
		}
	}
	if runtime.GOOS == "windows" {
		environment := "ucrt64"
		if runtime.GOARCH == "arm64" {
			environment = "clangarm64"
		}
		for _, root := range []string{os.Getenv("MSYS2_ROOT"), `C:\msys64`} {
			if root == "" {
				continue
			}
			for _, name := range names {
				path := filepath.Join(root, environment, "bin", name+".exe")
				if _, err := os.Stat(path); err == nil {
					return path, nil
				}
			}
		}
	}
	return "", fmt.Errorf("not found on PATH; use -qtpaths or -qmlls with the Qt 6 executable path")
}

func checkNativeModules(environment []string, qtBin string) error {
	var pkgConfig string
	var err error
	if runtime.GOOS == "windows" {
		pkgConfig = filepath.Join(qtBin, "pkg-config.exe")
		_, err = os.Stat(pkgConfig)
	} else {
		pkgConfig, err = exec.LookPath("pkg-config")
	}
	if err != nil {
		return fmt.Errorf("pkg-config is required for native and Qt Go analysis: %w", err)
	}
	for _, module := range []string{"gdal", "proj", "geos", "Qt6Core", "Qt6Quick", "Qt6Qml"} {
		command := exec.Command(pkgConfig, "--exists", module)
		command.Env = environment
		if output, err := command.CombinedOutput(); err != nil {
			return fmt.Errorf("pkg-config cannot find %s (%s): %w", module, strings.TrimSpace(string(output)), err)
		}
	}
	return nil
}

var versionPattern = regexp.MustCompile(`\b([0-9]+)\.([0-9]+)(?:\.[0-9]+)?\b`)

func checkQmllsVersion(path string) error {
	output, err := exec.Command(path, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("read qmlls version from %s: %w", path, err)
	}
	match := versionPattern.FindStringSubmatch(string(output))
	if match == nil {
		return fmt.Errorf("cannot identify qmlls version from %q", strings.TrimSpace(string(output)))
	}
	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	if major < 6 || major == 6 && minor < 8 {
		return fmt.Errorf("Qt VS Code extension requires qmlls 6.8 or newer; %s reports %s (use -qmlls with a newer executable)", path, match[0])
	}
	return nil
}
