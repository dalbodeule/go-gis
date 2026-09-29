[CmdletBinding()]
param(
    [ValidateSet('cli', 'native', 'desktop', 'desktop-native', 'all', 'all-native', 'clean')]
    [string]$Target = 'cli'
)

$ErrorActionPreference = 'Stop'
$RootDir = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$BuildDir = Join-Path $RootDir 'build'

if (-not $env:GOCACHE) {
    $env:GOCACHE = Join-Path ([System.IO.Path]::GetTempPath()) 'gogis-go-build'
}

function Invoke-GoBuild {
    param(
        [string[]]$Tags,
        [string]$Output,
        [string]$Package = './cmd/gis-cli'
    )

    New-Item -ItemType Directory -Force -Path $BuildDir | Out-Null
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
            if (Test-Path $BuildDir) {
                Remove-Item -Recurse -Force $BuildDir
            }
            return
        }
    }
    Write-Host "Build complete: $BuildDir"
}
finally {
    Pop-Location
}
