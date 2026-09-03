$ErrorActionPreference = "Stop"

$nsisCommand = Get-Command makensis -ErrorAction SilentlyContinue
if (-not $nsisCommand) {
    $nsisDirectory = "C:\Program Files (x86)\NSIS"
    if (-not (Test-Path (Join-Path $nsisDirectory "makensis.exe"))) {
        throw "NSIS is required. Install it with: winget install --id NSIS.NSIS --exact"
    }
    $env:Path = "$nsisDirectory;$env:Path"
}

$env:GOCACHE = Join-Path $PSScriptRoot ".cache\go-build"
$env:GOMODCACHE = Join-Path $PSScriptRoot ".cache\gomod"

Push-Location $PSScriptRoot
try {
    & wails build --nsis
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
} finally {
    Pop-Location
}
