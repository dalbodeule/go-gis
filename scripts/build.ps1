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
        'native' { Invoke-GoBuild -Tags @('native') -Output 'gis-cli-native.exe' }
        'desktop' { Invoke-GoBuild -Tags @('qt') -Output 'gogis-desktop.exe' -Package './cmd/gis-desktop' }
        'desktop-native' {
            Invoke-GoBuild -Tags @('qt', 'native') -Output 'gogis-desktop-native.exe' -Package './cmd/gis-desktop'
        }
        'all' {
            Invoke-GoBuild -Tags @() -Output 'gis-cli.exe'
            Invoke-GoBuild -Tags @('qt') -Output 'gogis-desktop.exe' -Package './cmd/gis-desktop'
        }
        'all-native' {
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
