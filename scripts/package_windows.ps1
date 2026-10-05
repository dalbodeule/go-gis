[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('amd64', 'arm64')]
    [string]$Arch,
    [string]$Version = '0.1.0-dev',
    [string]$MsysRoot = 'C:\tools\msys64',
    [switch]$BuildInstaller
)

$ErrorActionPreference = 'Stop'
$RootDir = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$PackageSlug = $Version -replace '[^A-Za-z0-9.-]', '-'
$PackageDir = Join-Path $RootDir "build\packages\windows\$Arch\GoGIS-$PackageSlug"
$ZipPath = Join-Path (Split-Path $PackageDir -Parent) "GoGIS-$PackageSlug-windows-$Arch.zip"

if (Test-Path -LiteralPath $PackageDir) {
    throw "Package directory already exists; choose another -Version or move it manually: $PackageDir"
}
if (Test-Path -LiteralPath $ZipPath) {
    throw "Package archive already exists; choose another -Version or move it manually: $ZipPath"
}

if ($Arch -eq 'amd64') {
    $Environment = 'ucrt64'
    $PackagePrefix = 'mingw-w64-ucrt-x86_64'
    $Compiler = 'gcc'
    $CXXCompiler = 'g++'
    $ExpectedGoArch = 'X64'
} else {
    $Environment = 'clangarm64'
    $PackagePrefix = 'mingw-w64-clang-aarch64'
    $Compiler = 'clang'
    $CXXCompiler = 'clang++'
    $ExpectedGoArch = 'Arm64'
}

$Prefix = Join-Path $MsysRoot $Environment
$MsysBin = Join-Path $MsysRoot 'usr\bin'
$BinDir = Join-Path $Prefix 'bin'
$QmlDir = Join-Path $RootDir 'cmd\gis-desktop\qml'
$RuntimeOSArch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
if ($RuntimeOSArch -ne $ExpectedGoArch) {
    throw "Building $Arch requires a native $Arch Windows host. Current OS architecture is $RuntimeOSArch."
}
if (-not (Test-Path -LiteralPath $BinDir -PathType Container)) {
    throw "MSYS2 $Environment environment was not found: $BinDir"
}

$RequiredTools = @(
    (Join-Path $BinDir "$Compiler.exe"),
    (Join-Path $BinDir "$CXXCompiler.exe"),
    (Join-Path $BinDir 'pkg-config.exe'),
    (Join-Path $BinDir 'windeployqt.exe'),
    (Join-Path $BinDir 'gdalinfo.exe'),
    (Join-Path $BinDir 'projinfo.exe'),
    (Join-Path $BinDir 'objdump.exe'),
    (Join-Path $MsysBin 'ldd.exe'),
    (Join-Path $MsysBin 'cygpath.exe')
)
foreach ($Tool in $RequiredTools) {
    if (-not (Test-Path -LiteralPath $Tool -PathType Leaf)) {
        throw "Required tool not found: $Tool"
    }
}

$env:Path = "$BinDir;$MsysBin;$env:Path"
$env:GOOS = 'windows'
$env:GOARCH = $Arch
$env:CGO_ENABLED = '1'
$env:CC = Join-Path $BinDir "$Compiler.exe"
$env:CXX = Join-Path $BinDir "$CXXCompiler.exe"
$env:CGO_CXXFLAGS = '-std=c++17'
$env:PKG_CONFIG_PATH = Join-Path $Prefix 'lib\pkgconfig'
$env:PROJ_DATA = Join-Path $Prefix 'share\proj'
$env:GDAL_DATA = Join-Path $Prefix 'share\gdal'
if (-not $env:GOCACHE) {
    $env:GOCACHE = Join-Path ([System.IO.Path]::GetTempPath()) 'gogis-go-build'
}

$GoArch = (& go env GOARCH).Trim()
if ($LASTEXITCODE -ne 0 -or $GoArch -ne $Arch) {
    throw "Go toolchain architecture is $GoArch, but package target is $Arch. Use the native Go distribution for that architecture."
}
foreach ($Module in @('gdal', 'proj', 'geos', 'Qt6Core', 'Qt6Quick', 'Qt6Qml')) {
    & (Join-Path $BinDir 'pkg-config.exe') --exists $Module
    if ($LASTEXITCODE -ne 0) {
        throw "pkg-config module is missing in ${Environment}: $Module"
    }
}

Write-Host "Building GoGIS Windows $Arch with MSYS2 $Environment"
Write-Host "GDAL $((& (Join-Path $BinDir 'gdalinfo.exe') --version) -join ' ')"
Write-Host "PROJ data: $env:PROJ_DATA"
Write-Host "GDAL data: $env:GDAL_DATA"

New-Item -ItemType Directory -Path $PackageDir -Force | Out-Null
$AppExe = Join-Path $PackageDir 'GoGIS.exe'
Push-Location $RootDir
try {
    & go build -trimpath -tags 'qt native' `
        -ldflags "-s -w -X main.appVersion=$Version" `
        -o $AppExe ./cmd/gis-desktop
    if ($LASTEXITCODE -ne 0) {
        throw "GoGIS $Arch build failed with exit code $LASTEXITCODE"
    }
} finally {
    Pop-Location
}

$DeployQt = Join-Path $BinDir 'windeployqt.exe'
& $DeployQt --verbose 0 --dir $PackageDir --qmldir $QmlDir --compiler-runtime $AppExe
if ($LASTEXITCODE -ne 0) {
    throw "windeployqt failed with exit code $LASTEXITCODE"
}
Copy-Item -LiteralPath (Join-Path $RootDir 'installer\windows\qt.conf') `
    -Destination (Join-Path $PackageDir 'qt.conf') -Force

$ResourcesDir = Join-Path $PackageDir 'resources'
New-Item -ItemType Directory -Path $ResourcesDir -Force | Out-Null
foreach ($Resource in @(
    @{ Source = (Join-Path $Prefix 'share\gdal'); Name = 'gdal' },
    @{ Source = (Join-Path $Prefix 'share\proj'); Name = 'proj' }
)) {
    if (-not (Test-Path -LiteralPath $Resource.Source -PathType Container)) {
        throw "Required GIS resource directory is missing: $($Resource.Source)"
    }
    Copy-Item -LiteralPath $Resource.Source -Destination (Join-Path $ResourcesDir $Resource.Name) -Recurse
}

$GdalPluginSource = Join-Path $Prefix 'lib\gdalplugins'
$GdalPluginTarget = Join-Path $ResourcesDir 'gdalplugins'
if (Test-Path -LiteralPath $GdalPluginSource -PathType Container) {
    Copy-Item -LiteralPath $GdalPluginSource -Destination $GdalPluginTarget -Recurse
} else {
    New-Item -ItemType Directory -Path $GdalPluginTarget | Out-Null
}

# Resolve the executable's native DLL closure and any GDAL runtime plugins.
# windeployqt handles Qt's plugin/QML deployment; verify the direct imports of
# every staged DLL afterward and resolve any additional non-Windows dependency.
$Ldd = Join-Path $MsysBin 'ldd.exe'
$Cygpath = Join-Path $MsysBin 'cygpath.exe'
$Objdump = Join-Path $BinDir 'objdump.exe'
$WindowsRoot = [System.IO.Path]::GetFullPath($env:WINDIR).TrimEnd('\') + '\'
function Resolve-DllClosure {
    param([Parameter(Mandatory = $true)][string]$Path)

    $LddOutput = & $Ldd $Path 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "Unable to inspect runtime imports for $Path`: $($LddOutput -join ' ')"
    }
    foreach ($Line in $LddOutput) {
        $Text = [string]$Line
        if ($Text -match '^\s*([^=]+?)\s+=>\s+not found') {
            throw "Unresolved runtime DLL $($Matches[1]) imported by $Path"
        }
        if ($Text -notmatch '^\s*[^=]+?\s+=>\s+(?<path>\S+)\s+\(') { continue }

        $MsysPath = $Matches['path']
        if (-not $MsysPath.StartsWith('/')) { continue }
        $ResolvedPath = (& $Cygpath -w $MsysPath).Trim()
        if (-not (Test-Path -LiteralPath $ResolvedPath -PathType Leaf)) { continue }
        $ResolvedPath = (Resolve-Path -LiteralPath $ResolvedPath).Path
        if ($ResolvedPath.StartsWith($WindowsRoot, [System.StringComparison]::OrdinalIgnoreCase)) { continue }
        if ($ResolvedPath.StartsWith((Join-Path $MsysRoot 'usr\bin') + '\', [System.StringComparison]::OrdinalIgnoreCase)) {
            throw "A runtime dependency came from the MSYS POSIX runtime and cannot be redistributed this way: $ResolvedPath"
        }

        $Destination = Join-Path $PackageDir (Split-Path $ResolvedPath -Leaf)
        if (Test-Path -LiteralPath $Destination -PathType Leaf) {
            $SourceHash = (Get-FileHash -LiteralPath $ResolvedPath -Algorithm SHA256).Hash
            $TargetHash = (Get-FileHash -LiteralPath $Destination -Algorithm SHA256).Hash
            if ($SourceHash -ne $TargetHash) {
                throw "Runtime DLL name collision for $Destination from $ResolvedPath"
            }
            continue
        }
        Copy-Item -LiteralPath $ResolvedPath -Destination $Destination
    }
}

Resolve-DllClosure -Path $AppExe
$GdalPluginBinaries = @(Get-ChildItem -LiteralPath $GdalPluginTarget -File -Filter '*.dll' -Recurse -ErrorAction SilentlyContinue)
foreach ($Plugin in $GdalPluginBinaries) {
    Resolve-DllClosure -Path $Plugin.FullName
}

$ScannedImportFiles = [System.Collections.Generic.HashSet[string]]::new([System.StringComparer]::OrdinalIgnoreCase)
$SystemDlls = [System.Collections.Generic.HashSet[string]]::new([System.StringComparer]::OrdinalIgnoreCase)
Get-ChildItem -LiteralPath (Join-Path $env:WINDIR 'System32') -File -Filter '*.dll' |
    ForEach-Object { [void]$SystemDlls.Add($_.Name) }
while ($true) {
    $MissingImports = [System.Collections.Generic.HashSet[string]]::new([System.StringComparer]::OrdinalIgnoreCase)
    $PackagedDllNames = [System.Collections.Generic.HashSet[string]]::new([System.StringComparer]::OrdinalIgnoreCase)
    Get-ChildItem -LiteralPath $PackageDir -File -Filter '*.dll' -Recurse |
        ForEach-Object { [void]$PackagedDllNames.Add($_.Name) }

    $Candidates = @(
        Get-ChildItem -LiteralPath $PackageDir -File -Recurse |
            Where-Object { $_.Extension -in @('.exe', '.dll') } |
            Where-Object { -not $ScannedImportFiles.Contains($_.FullName) }
    )
    if ($Candidates.Count -eq 0) { break }

    foreach ($Candidate in $Candidates) {
        [void]$ScannedImportFiles.Add($Candidate.FullName)
        $ImportOutput = & $Objdump -p $Candidate.FullName 2>&1
        if ($LASTEXITCODE -ne 0) {
            throw "Unable to inspect DLL imports for $($Candidate.FullName): $($ImportOutput -join ' ')"
        }
        foreach ($Line in $ImportOutput) {
            if ([string]$Line -notmatch '^\s*DLL Name:\s*(?<name>\S+)') { continue }
            $ImportName = $Matches['name']
            if ($ImportName -match '^(api-ms-win-|ext-ms-win-)') { continue }
            if ($PackagedDllNames.Contains($ImportName) -or $SystemDlls.Contains($ImportName)) { continue }
            [void]$MissingImports.Add($ImportName)
        }
    }

    foreach ($ImportName in $MissingImports) {
        $ImportSource = Join-Path $BinDir $ImportName
        if (-not (Test-Path -LiteralPath $ImportSource -PathType Leaf)) {
            throw "Runtime dependency $ImportName is neither packaged nor present in Windows System32 or $BinDir"
        }
        $ImportTarget = Join-Path $PackageDir $ImportName
        if (-not (Test-Path -LiteralPath $ImportTarget -PathType Leaf)) {
            Copy-Item -LiteralPath $ImportSource -Destination $ImportTarget
        }
        Resolve-DllClosure -Path $ImportSource
    }
}

$LicenseSource = Join-Path $Prefix 'share\licenses'
if (Test-Path -LiteralPath $LicenseSource -PathType Container) {
    Copy-Item -LiteralPath $LicenseSource -Destination (Join-Path $PackageDir 'licenses') -Recurse
} else {
    throw "MSYS2 license directory is missing: $LicenseSource"
}

$BuildInfo = @(
    "GoGIS $Version",
    "Target: windows/$Arch",
    "Go: $((& go version) -join ' ')",
    "MSYS2 environment: $Environment",
    "GDAL: $((& (Join-Path $BinDir 'pkg-config.exe') --modversion gdal) -join ' ')",
    "PROJ: $((& (Join-Path $BinDir 'pkg-config.exe') --modversion proj) -join ' ')",
    "GEOS: $((& (Join-Path $BinDir 'pkg-config.exe') --modversion geos) -join ' ')",
    "Qt: $((& (Join-Path $BinDir 'pkg-config.exe') --modversion Qt6Core) -join ' ')",
    'License texts for the MSYS2 runtime packages are in licenses/.'
)
Set-Content -LiteralPath (Join-Path $PackageDir 'BUILD-INFO.txt') -Value $BuildInfo -Encoding utf8
Compress-Archive -Path (Join-Path $PackageDir '*') -DestinationPath $ZipPath -CompressionLevel Optimal

Write-Host "Portable package: $PackageDir"
Write-Host "Portable ZIP: $ZipPath"

if ($BuildInstaller) {
    $Iscc = Get-Command ISCC.exe -ErrorAction SilentlyContinue
    if (-not $Iscc) {
        throw 'Inno Setup compiler ISCC.exe was not found; install Inno Setup 6 or omit -BuildInstaller.'
    }
    $env:GOGIS_VERSION = $PackageSlug
    $env:GOGIS_ARCH = if ($Arch -eq 'amd64') { 'x64os' } else { 'arm64' }
    $env:GOGIS_ARCH_LABEL = $Arch
    $env:GOGIS_PACKAGE_DIR = Split-Path $PackageDir -Parent
    $InstallerScript = Join-Path $RootDir 'installer\windows\GoGIS.iss'
    & $Iscc.Source $InstallerScript
    if ($LASTEXITCODE -ne 0) {
        throw "Inno Setup compilation failed with exit code $LASTEXITCODE"
    }
}
