[CmdletBinding()]
param(
    [switch]$Native,
    [switch]$Qt,
    [switch]$SkipRace
)

$ErrorActionPreference = 'Stop'
$RootDir = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$env:CGO_ENABLED = '1'

function Invoke-Checked {
    param(
        [string]$Command,
        [string[]]$Arguments
    )

    & $Command @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$Command $($Arguments -join ' ') failed with exit code $LASTEXITCODE"
    }
}

function Assert-File {
    param([string]$Path)
    if (-not (Test-Path (Join-Path $RootDir $Path) -PathType Leaf)) {
        throw "required file is missing: $Path"
    }
}

Push-Location $RootDir
try {
    Assert-File 'LICENSE'
    Assert-File 'agents.md'
    Assert-File 'testdata/sample.geojson'

    Invoke-Checked 'go' @('test', './...')
    Invoke-Checked 'go' @('vet', './...')
    if (-not $SkipRace) {
        Invoke-Checked 'go' @('test', '-race', './...')
    }

    & "$PSScriptRoot/build.ps1" cli
    if ($LASTEXITCODE -ne 0) {
        throw 'portable CLI build failed'
    }

    if ($Native) {
        Invoke-Checked 'go' @('test', '-tags', 'native', './...')
        if (-not $SkipRace) {
            Invoke-Checked 'go' @('test', '-race', '-tags', 'native', './...')
        }
        & "$PSScriptRoot/build.ps1" native
        if ($LASTEXITCODE -ne 0) {
            throw 'native CLI build failed'
        }

        Push-Location (Join-Path $RootDir 'third_party/godal')
        try {
            Invoke-Checked 'go' @('vet', './...')
            Invoke-Checked 'go' @('test', './...', '-skip', '^TestVSIGCSNoAuth$')
            if (-not $SkipRace) {
                Invoke-Checked 'go' @('test', '-race', './...', '-skip', '^TestVSIGCSNoAuth$')
            }
        }
        finally {
            Pop-Location
        }
    }

    if ($Qt) {
        $previousCxxFlags = $env:CGO_CXXFLAGS
        if (-not $env:CGO_CXXFLAGS) {
            $env:CGO_CXXFLAGS = '-std=c++17'
        }
        try {
            $qtTags = 'qt'
            if ($Native) { $qtTags = 'qt native' }
            Invoke-Checked 'go' @('test', '-tags', $qtTags, './cmd/gis-desktop')
            if ($Native) {
                $previousRepeatedViewportStress = $env:GOGIS_TEST_REPEATED_VIEWPORT_1M
                try {
                    $env:GOGIS_TEST_REPEATED_VIEWPORT_1M = '1'
                    Invoke-Checked 'go' @('test', '-tags', $qtTags, './cmd/gis-desktop', '-run', '^TestWindowedReadOnlyRepeatedViewportMoves1M$', '-count=1')
                }
                finally {
                    $env:GOGIS_TEST_REPEATED_VIEWPORT_1M = $previousRepeatedViewportStress
                }
            }
        }
        finally {
            $env:CGO_CXXFLAGS = $previousCxxFlags
        }
    }

    Invoke-Checked 'git' @('diff', '--check')
    Write-Host 'Verification complete'
}
finally {
    Pop-Location
}
