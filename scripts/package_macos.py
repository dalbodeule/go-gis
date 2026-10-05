#!/usr/bin/env python3
"""Build an unsigned, internal-test macOS app bundle and compressed DMG."""

from __future__ import annotations

import datetime as dt
import filecmp
import hashlib
import os
from pathlib import Path
import plistlib
import re
import shutil
import subprocess
import tempfile


ROOT = Path(__file__).resolve().parent.parent
BUILD = ROOT / "build"
EXECUTABLE_NAME = "GoGIS"


def run(args: list[str], *, capture: bool = False) -> str:
    result = subprocess.run(
        args,
        check=True,
        text=True,
        stdout=subprocess.PIPE if capture else None,
        stderr=subprocess.PIPE if capture else None,
    )
    return result.stdout if capture else ""


def brew_prefix(formula: str) -> Path:
    return Path(run(["brew", "--prefix", formula], capture=True).strip())


def is_system_path(path: str) -> bool:
    return path.startswith(("/System/", "/usr/lib/"))


def load_commands(binary: Path) -> list[str]:
    output = run(["otool", "-L", str(binary)], capture=True)
    dependencies: list[str] = []
    for line in output.splitlines()[1:]:
        match = re.match(r"\s*(.+?) \(compatibility version ", line)
        if match:
            dependencies.append(match.group(1))
    return dependencies


def run_paths(binary: Path) -> list[str]:
    output = run(["otool", "-l", str(binary)], capture=True)
    lines = output.splitlines()
    result: list[str] = []
    for index, line in enumerate(lines):
        if line.strip() != "cmd LC_RPATH":
            continue
        for candidate in lines[index + 1 : index + 5]:
            match = re.search(r"\bpath (.+?) \(offset \d+\)", candidate)
            if match:
                result.append(match.group(1))
                break
    return result


def expand_runtime_path(value: str, loader: Path, executable_dir: Path) -> Path:
    if value == "@loader_path":
        return loader.parent
    if value.startswith("@loader_path/"):
        return loader.parent / value.removeprefix("@loader_path/")
    if value == "@executable_path":
        return executable_dir
    if value.startswith("@executable_path/"):
        return executable_dir / value.removeprefix("@executable_path/")
    return Path(value)


def resolve_dependency(
    dependency: str,
    loader: Path,
    executable: Path,
    app: Path,
) -> Path | None:
    executable_dir = executable.parent
    candidates: list[Path] = []
    if dependency.startswith("/"):
        candidates.append(Path(dependency))
    elif dependency.startswith("@loader_path") or dependency.startswith("@executable_path"):
        candidates.append(expand_runtime_path(dependency, loader, executable_dir))
    elif dependency.startswith("@rpath/"):
        suffix = dependency.removeprefix("@rpath/")
        for runpath in run_paths(loader) + ([] if loader == executable else run_paths(executable)):
            candidates.append(expand_runtime_path(runpath, loader, executable_dir) / suffix)
        # Homebrew's aggregate `qt` formula provides module frameworks in its
        # lib directory even when their QML plugin lives in another formula.
        candidates.append(Path("/opt/homebrew/opt/qt/lib") / suffix)
        candidates.append(app / "Contents" / "Frameworks" / suffix)
    else:
        return None

    for candidate in candidates:
        if candidate.exists():
            return candidate.resolve()
    return None


def macho_files(app: Path) -> list[Path]:
    # Mach-O magic values, in native and fat/universal byte order. Avoid
    # launching `file` once per resource: the bundled PROJ database contains
    # hundreds of files and makes that approach needlessly slow.
    macho_headers = {
        b"\xfe\xed\xfa\xce", b"\xce\xfa\xed\xfe",
        b"\xfe\xed\xfa\xcf", b"\xcf\xfa\xed\xfe",
        b"\xca\xfe\xba\xbe", b"\xbe\xba\xfe\xca",
        b"\xca\xfe\xba\xbf", b"\xbf\xba\xfe\xca",
    }
    result: list[Path] = []
    for path in app.rglob("*"):
        if not path.is_file() or path.is_symlink():
            continue
        try:
            with path.open("rb") as binary:
                if binary.read(4) in macho_headers:
                    result.append(path)
        except OSError:
            continue
    return result


def bundled_reference(loader: Path, dependency_path: Path, executable: Path) -> str:
    if loader == executable:
        prefix = "@executable_path"
        base = executable.parent
    else:
        prefix = "@loader_path"
        base = loader.parent
    return f"{prefix}/{os.path.relpath(dependency_path, start=base)}"


def normalize_framework_aliases(frameworks: Path) -> None:
    # Some Homebrew QML plugins refer to a flat Contents/Frameworks/QtCore
    # path while macdeployqt also installs QtCore.framework. Keep one image in
    # the process by making the flat name a relative alias to the framework.
    for alias in frameworks.iterdir():
        if not alias.is_file() or alias.is_symlink():
            continue
        framework_binary = frameworks / f"{alias.name}.framework" / "Versions" / "A" / alias.name
        if not framework_binary.is_file():
            continue
        alias.unlink()
        alias.symlink_to(os.path.relpath(framework_binary, start=frameworks))


def bundle_non_system_libraries(app: Path, executable: Path) -> int:
    frameworks = app / "Contents" / "Frameworks"
    bundled: dict[str, Path] = {}
    rewrites: list[tuple[Path, str, str]] = []
    scanned: set[Path] = set()
    pending = macho_files(app)

    while pending:
        loader = pending.pop()
        loader = loader.resolve()
        if loader in scanned:
            continue
        scanned.add(loader)
        for dependency in load_commands(loader):
            if is_system_path(dependency):
                continue
            resolved = resolve_dependency(dependency, loader, executable, app)
            if resolved is None:
                if dependency.startswith(("@rpath/", "@loader_path/", "@executable_path/")):
                    raise RuntimeError(f"could not resolve bundled dependency {dependency!r} from {loader}")
                raise RuntimeError(f"missing dynamic dependency {dependency!r} from {loader}")
            try:
                resolved.relative_to(app.resolve())
                replacement = bundled_reference(loader, resolved, executable)
                if dependency != replacement:
                    rewrites.append((loader, dependency, replacement))
                continue
            except ValueError:
                pass

            library_name = Path(dependency).name
            destination = frameworks / library_name
            previous = bundled.get(library_name)
            if previous is not None and previous != resolved and not filecmp.cmp(
                previous, resolved, shallow=False
            ):
                digest = hashlib.sha256(str(resolved).encode()).hexdigest()[:10]
                destination = frameworks / f"{destination.stem}-{digest}{destination.suffix}"
                previous = bundled.get(destination.name)
            if previous is None:
                if destination.exists() and not os.path.samefile(destination, resolved):
                    if not filecmp.cmp(resolved, destination, shallow=False):
                        digest = hashlib.sha256(str(resolved).encode()).hexdigest()[:10]
                        destination = frameworks / f"{destination.stem}-{digest}{destination.suffix}"
                        previous = bundled.get(destination.name)
                if previous is None and not destination.exists():
                    shutil.copy2(resolved, destination)
                if previous is None:
                    bundled[destination.name] = resolved
                    pending.append(destination)
            elif previous != resolved:
                if not filecmp.cmp(previous, resolved, shallow=False):
                    raise RuntimeError(f"unable to assign a unique bundle name to {resolved}")

            replacement = bundled_reference(loader, destination, executable)
            rewrites.append((loader, dependency, replacement))

    for loader, old_name, new_name in rewrites:
        if old_name != new_name:
            run(["install_name_tool", "-change", old_name, new_name, str(loader)])
    for name, source in bundled.items():
        run(["install_name_tool", "-id", f"@rpath/{name}", str(frameworks / name)])
    return len(bundled)


def create_app(stage: Path) -> Path:
    app = stage / "GoGIS.app"
    contents = app / "Contents"
    macos = contents / "MacOS"
    resources = contents / "Resources"
    frameworks = contents / "Frameworks"
    macos.mkdir(parents=True)
    resources.mkdir(parents=True)
    frameworks.mkdir(parents=True)

    source = BUILD / "gogis-desktop-native"
    executable = macos / EXECUTABLE_NAME
    shutil.copy2(source, executable)
    plist = {
        "CFBundleDevelopmentRegion": "en",
        "CFBundleExecutable": EXECUTABLE_NAME,
        "CFBundleIdentifier": "org.gogis.desktop",
        "CFBundleInfoDictionaryVersion": "6.0",
        "CFBundleName": "GoGIS",
        "CFBundlePackageType": "APPL",
        "CFBundleShortVersionString": "0.1.0",
        "CFBundleVersion": "0.1.0",
        "LSApplicationCategoryType": "public.app-category.utilities",
        "NSHighResolutionCapable": True,
        "NSPrincipalClass": "NSApplication",
    }
    with (contents / "Info.plist").open("wb") as handle:
        plistlib.dump(plist, handle, sort_keys=True)

    run([
        "macdeployqt", str(app),
        f"-qmldir={ROOT / 'cmd/gis-desktop/qml'}",
        f"-libpath={brew_prefix('qtbase') / 'lib'}",
        f"-libpath={brew_prefix('qtdeclarative') / 'lib'}",
        f"-libpath={brew_prefix('qt') / 'lib'}",
        "-no-plugins",
        "-no-codesign",
    ])

    # GoGIS uses Qt Quick Controls but no image, PDF, or multimedia plugins.
    # Deploy only the platform plugin required to create a macOS window.
    plugin_root = app / "Contents" / "PlugIns"
    if plugin_root.exists():
        for plugin_group in plugin_root.iterdir():
            if plugin_group.name not in {"quick", "platforms"}:
                if plugin_group.is_dir():
                    shutil.rmtree(plugin_group)
                else:
                    plugin_group.unlink()
    platform_plugins = plugin_root / "platforms"
    platform_plugins.mkdir(parents=True, exist_ok=True)
    cocoa_plugin = brew_prefix("qtbase") / "share" / "qt" / "plugins" / "platforms" / "libqcocoa.dylib"
    if not cocoa_plugin.is_file():
        raise RuntimeError(f"Qt macOS platform plugin not found: {cocoa_plugin}")
    shutil.copy2(cocoa_plugin, platform_plugins / cocoa_plugin.name)
    normalize_framework_aliases(frameworks)

    proj = brew_prefix("proj")
    gdal = brew_prefix("gdal")
    # Resolve Homebrew's versioned data symlinks into regular bundle files;
    # links left pointing into /opt/homebrew would make the app non-portable.
    shutil.copytree(proj / "share" / "proj", resources / "proj")
    shutil.copytree(gdal / "share" / "gdal", resources / "gdal")
    plugin_dir = gdal / "lib" / "gdalplugins"
    if plugin_dir.is_dir():
        shutil.copytree(plugin_dir, resources / "gdalplugins")

    bundle_non_system_libraries(app, executable)
    # The dependency walk can add flat Qt aliases after macdeployqt; normalize
    # again so all plugin references load the same framework image.
    normalize_framework_aliases(frameworks)
    run(["codesign", "--force", "--deep", "--sign", "-", str(app)])
    return app


def main() -> None:
    if os.uname().machine != "arm64":
        raise SystemExit("this packaging pilot currently targets macOS ARM64 only")
    if shutil.which("brew") is None or shutil.which("macdeployqt") is None:
        raise SystemExit("Homebrew and macdeployqt are required")

    run([str(ROOT / "scripts" / "build.sh"), "desktop-native"])
    package_root = BUILD / "packages" / "macos-arm64"
    package_root.mkdir(parents=True, exist_ok=True)
    stamp = dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    stem = f"GoGIS-0.1.0-dev-macos-arm64-{stamp}"
    app_output = package_root / f"{stem}.app"
    dmg_output = package_root / f"{stem}.dmg"

    with tempfile.TemporaryDirectory(prefix="gogis-macos-package-", dir=BUILD) as temporary:
        stage = Path(temporary)
        app = create_app(stage)
        shutil.copytree(app, app_output, symlinks=True)
        run([
            "hdiutil", "create", "-quiet", "-format", "UDZO",
            "-volname", "GoGIS", "-srcfolder", str(app_output), str(dmg_output),
        ])
        print(f"Created internal-test app bundle: {app_output}")
        print(f"Created internal-test DMG: {dmg_output}")
        print("This unsigned package is for internal testing, not public redistribution.")


if __name__ == "__main__":
    main()
