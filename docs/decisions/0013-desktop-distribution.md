# ADR 0013: Desktop release packaging before single-binary linking

## Status

Accepted for the Windows portable-package pilot. Installer compilation, code
signing, and clean-machine validation remain release gates.

## Evidence in this checkout

On macOS arm64 (2026-10-04), the native desktop executable is about 56 MiB.
`otool -L build/gogis-desktop-native` shows dynamic links to Qt Widgets/Gui/
Core/Qml/Quick, GDAL, PROJ, and GEOS from Homebrew. The Go embed includes
Main.qml but not the Qt QML import modules or platform plugins. PROJ also
requires an accessible `proj.db`; GDAL may need its data directory. A copy of
this executable alone is therefore **not** a portable desktop release.
See the [Qt deployment overview](https://doc.qt.io/qt-6/deployment.html),
[PROJ resources](https://proj.org/en/stable/resource_files.html), and
[GDAL_DATA configuration](https://gdal.org/en/stable/user/configoptions.html).

## Options and decision

| Form | User sees | Current feasibility | Decision |
| --- | --- | --- | --- |
| One statically linked executable | One true binary | Would require compatible static Qt/GDAL/PROJ/GEOS builds, QML/plugin registration, data embedding/extraction, and a fresh license review. Not supported by current build. | Defer. |
| Portable folder/ZIP | Extract and run, no installer | Can carry shared libraries, Qt plugins/QML modules, `proj.db`, and GDAL data together. Needs relocation and clean-machine tests. | First cross-platform release candidate. |
| macOS `.app` in `.dmg` | One Finder app item/disk image, many internal files | Bundle Qt with `macdeployqt -qmldir=cmd/gis-desktop/qml`; add non-Qt GIS libraries and data separately, repair relative library paths, then sign/notarize for public distribution. | Preferred macOS deliverable after portable-folder pilot. |
| Windows installer (MSIX or conventional setup) | Install/uninstall workflow | Start from a verified portable directory produced with `windeployqt --qmldir`; it does **not** collect all third-party GIS DLLs. Wrap the verified directory only after clean-Windows testing. | Preferred Windows deliverable after ZIP pilot. |
| Linux AppImage / distro package | One downloadable image or managed install | AppImage is one distributable file, **not** one linked binary; it still carries libraries inside. A `.deb`/`.rpm` can instead depend on distro libraries but has version/ABI constraints. | Pilot AppImage on a defined baseline; add distro packages if needed. |

Qt's [macOS deployment tool](https://doc.qt.io/qt-6/macos-deployment.html)
and [Windows deployment tool](https://doc.qt.io/qt-6/windows-deployment.html)
handle Qt frameworks/plugins/QML, but third-party GIS dependencies need their
own inventory. [Qt Linux deployment](https://doc.qt.io/qt-6/linux-deployment.html)
describes shared-library/plugin layout; the
[AppImage format](https://docs.appimage.org/introduction/concepts.html)
provides the one-download-file option. Microsoft describes
[MSIX](https://learn.microsoft.com/en-us/windows/msix/overview) as an
installer/update format, not a replacement for collecting runtime dependencies.

## Release gate

1. Build separately for each OS/architecture with one consistent native ABI.
   Inventory direct and transitive dependencies (`otool -L`,
   `dumpbin /dependents`, or `ldd`) and dynamic GIS drivers/plugins. Do not copy
   developer-machine absolute paths into the artifact.
2. Include and locate Qt platform/QML plugins, `proj.db` and required grids,
   GDAL data, and license notices. Use relocatable paths or a launcher scoped
   to the package; do not require Homebrew, OSGeo4W, or Qt on the target.
3. Test on a clean machine/VM without development dependencies: start UI,
   open SHP and GeoPackage, search EPSG catalog, transform a layer, choose a
   color, save a project, and export DXF.
4. Check each bundled component's redistribution terms. Qt's
   [licensing guide](https://doc.qt.io/qt-6/licensing.html) notes LGPL/GPL/
   commercial choices and module-specific terms; static linking needs a
   separate compliance review. This ADR is an engineering direction, not a
   legal conclusion.
5. For public macOS distribution, sign the final nested binaries/bundle and
   notarize using the [Apple Developer ID workflow](https://developer.apple.com/developer-id/).
   For Windows installers, decide signing and update-channel policy before
   publishing. Do not claim the release gate passed from a build alone.

## Windows package pilot

`scripts/package_windows.ps1` builds on the target architecture, runs
`windeployqt` over the embedded-QML desktop app, copies the GDAL/PROJ data and
MSYS2 license texts, resolves the non-Windows DLL dependency closure, and emits
a directory plus ZIP. `installer/windows/GoGIS.iss` provides a per-architecture
Inno Setup 6 definition; it is compiled only when the local `ISCC.exe` is
available and `-BuildInstaller` is requested. The app locates its GIS data in a
`resources` directory next to `GoGIS.exe`, while explicit environment overrides
remain supported.

The manual `.github/workflows/package-windows.yml` builds AMD64 on
`windows-2025` with UCRT64 and ARM64 on `windows-11-arm` with CLANGARM64. Each
job builds natively, compiles the Inno Setup 6 installer, and uploads an expiring
Actions artifact; it does not publish a release or sign binaries. This checkout's installed MSYS2 environment is
UCRT64/AMD64 only, so ARM64 output and its runtime behavior are not verified
until the ARM64 job runs. Windows ARM64 MSYS2 support is preliminary; treat the
job as a compatibility gate, not proof of support before it passes.

Official references: [GitHub-hosted runner labels](https://docs.github.com/en/actions/reference/runners/github-hosted-runners),
[MSYS2 ARM64 support](https://www.msys2.org/docs/arm64/), and
[Inno Setup architecture identifiers](https://jrsoftware.org/ishelp/topic_archidentifiers.htm).
