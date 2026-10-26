[CmdletBinding()]
param(
    [ValidateSet('cli', 'native', 'desktop', 'desktop-native', 'all', 'all-native', 'clean')]
    [string]$Target = 'cli'
)

$ErrorActionPreference = 'Stop'
$RootDir = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$BuildDir = Join-Path $RootDir 'build'

function Assert-SafeBuildDirectory {
    $expectedPath = [System.IO.Path]::GetFullPath($BuildDir)
    if (-not (Test-Path -LiteralPath $BuildDir)) {
        return
    }

    $item = Get-Item -LiteralPath $BuildDir -Force
    if (-not $item.PSIsContainer) {
        throw "Refusing to use a non-directory build path: $BuildDir"
    }
    if (($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "Refusing to use a symlinked or junction build directory: $BuildDir"
    }
    $actualPath = [System.IO.Path]::GetFullPath($item.FullName)
    if (-not [string]::Equals($actualPath, $expectedPath, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "Refusing unexpected build directory target: $actualPath"
    }
    $nestedReparsePoint = Get-ChildItem -LiteralPath $BuildDir -Force -Recurse -ErrorAction Stop |
        Where-Object { ($_.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0 } |
        Select-Object -First 1
    if ($null -ne $nestedReparsePoint) {
        throw "Refusing build directory containing a symlink or junction: $($nestedReparsePoint.FullName)"
    }
}

function Ensure-BuildDirectory {
    Assert-SafeBuildDirectory
    New-Item -ItemType Directory -Force -Path $BuildDir | Out-Null
    Assert-SafeBuildDirectory
}

if (-not $env:GOCACHE) {
    $env:GOCACHE = Join-Path ([System.IO.Path]::GetTempPath()) 'gogis-go-build'
}

function Initialize-CgoEnvironment {
    param([bool]$NeedsQt, [bool]$NeedsNative)

    $goArch = (& go env GOARCH).Trim()
    if ($LASTEXITCODE -ne 0) {
        throw 'Could not determine Go target architecture.'
    }
    $msysEnvironment = switch ($goArch) {
        'amd64' { 'ucrt64' }
        'arm64' { 'clangarm64' }
        default { $null }
    }
    $compiler = if ($goArch -eq 'arm64') { 'clang.exe' } else { 'gcc.exe' }
    $cxxCompiler = if ($goArch -eq 'arm64') { 'clang++.exe' } else { 'g++.exe' }

    # Respect a caller-configured toolchain. Otherwise use a complete MSYS2
    # environment, rather than mixing its GIS libraries with another compiler.
    if (-not $env:CC -and -not (Get-Command $compiler -ErrorAction SilentlyContinue) -and $msysEnvironment) {
        $roots = @($env:MSYS2_ROOT, 'C:\tools\msys64', 'C:\msys64') |
            Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
        foreach ($root in $roots) {
            $prefix = Join-Path $root $msysEnvironment
            $bin = Join-Path $prefix 'bin'
            if (-not (Test-Path -LiteralPath (Join-Path $bin $compiler) -PathType Leaf) -or
                -not (Test-Path -LiteralPath (Join-Path $bin $cxxCompiler) -PathType Leaf) -or
                -not (Test-Path -LiteralPath (Join-Path $bin 'pkg-config.exe') -PathType Leaf)) {
                continue
            }
            $env:Path = "$bin;$(Join-Path $root 'usr\bin');$env:Path"
            $env:CC = Join-Path $bin $compiler
            $env:CXX = Join-Path $bin $cxxCompiler
            if (-not $env:PKG_CONFIG_PATH) { $env:PKG_CONFIG_PATH = Join-Path $prefix 'lib\pkgconfig' }
            if (-not $env:PROJ_DATA) { $env:PROJ_DATA = Join-Path $prefix 'share\proj' }
            if (-not $env:GDAL_DATA) { $env:GDAL_DATA = Join-Path $prefix 'share\gdal' }
            Write-Host "Using MSYS2 $msysEnvironment from $root"
            break
        }
    }

    $env:CGO_ENABLED = '1'
    $ccName = if ($env:CC) { $env:CC } else { $compiler }
    if (-not (Get-Command $ccName -ErrorAction SilentlyContinue)) {
        throw "CGO compiler not found: $ccName. Install an architecture-matched MSYS2 toolchain or configure CC and PATH."
    }
    $pkgConfigName = if ($env:PKG_CONFIG) { $env:PKG_CONFIG } else { 'pkg-config' }
    if (-not (Get-Command $pkgConfigName -ErrorAction SilentlyContinue)) {
        throw "pkg-config not found. Install GDAL, PROJ, GEOS and their development packages; see docs/build.md."
    }
    $modules = @()
    if ($NeedsNative) { $modules += @('gdal', 'proj', 'geos') }
    if ($NeedsQt) {
        $modules += @('Qt6Core', 'Qt6Quick', 'Qt6Qml')
        $cxxName = if ($env:CXX) { $env:CXX } else { $cxxCompiler }
        if (-not (Get-Command $cxxName -ErrorAction SilentlyContinue)) {
            throw "C++ compiler not found: $cxxName"
        }
        if ($env:CGO_CXXFLAGS -notmatch '(?:^|\s)-std=(?:gnu\+\+|c\+\+)17(?:\s|$)') {
            $env:CGO_CXXFLAGS = ($env:CGO_CXXFLAGS + ' -std=c++17').Trim()
        }
    }
    foreach ($module in $modules) {
        & $pkgConfigName --exists $module
        if ($LASTEXITCODE -ne 0) {
            throw "pkg-config module $module not found. Check the selected toolchain and PKG_CONFIG_PATH; see docs/build.md."
        }
    }
}

function Invoke-GoBuild {
    param(
        [string[]]$Tags,
        [string]$Output,
        [string]$Package = './cmd/gis-cli'
    )

    Ensure-BuildDirectory
    $arguments = @('build')
    if ($Tags.Count -gt 0) {
        $arguments += @('-tags', ($Tags -join ' '))
    }
    $arguments += @('-o', (Join-Path $BuildDir $Output), $Package)
    & go @arguments
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed with exit code $LASTEXITCODE"
    }
}

Push-Location $RootDir
try {
    switch ($Target) {
        'cli' { Invoke-GoBuild -Tags @() -Output 'gis-cli.exe' }
        'native' {
            Initialize-CgoEnvironment -NeedsQt $false -NeedsNative $true
            Invoke-GoBuild -Tags @('native') -Output 'gis-cli-native.exe'
        }
        'desktop' {
            Initialize-CgoEnvironment -NeedsQt $true -NeedsNative $false
            Invoke-GoBuild -Tags @('qt') -Output 'gogis-desktop.exe' -Package './cmd/gis-desktop'
        }
        'desktop-native' {
            Initialize-CgoEnvironment -NeedsQt $true -NeedsNative $true
            Invoke-GoBuild -Tags @('qt', 'native') -Output 'gogis-desktop-native.exe' -Package './cmd/gis-desktop'
        }
        'all' {
            Invoke-GoBuild -Tags @() -Output 'gis-cli.exe'
            Initialize-CgoEnvironment -NeedsQt $true -NeedsNative $false
            Invoke-GoBuild -Tags @('qt') -Output 'gogis-desktop.exe' -Package './cmd/gis-desktop'
        }
        'all-native' {
            Initialize-CgoEnvironment -NeedsQt $true -NeedsNative $true
            Invoke-GoBuild -Tags @('native') -Output 'gis-cli-native.exe'
            Invoke-GoBuild -Tags @('qt', 'native') -Output 'gogis-desktop-native.exe' -Package './cmd/gis-desktop'
        }
        'clean' {
            if (Test-Path -LiteralPath $BuildDir) {
                Assert-SafeBuildDirectory
                Remove-Item -LiteralPath $BuildDir -Recurse -Force
            }
            return
        }
    }
    Write-Host "Build complete: $BuildDir"
}
finally {
    Pop-Location
}
