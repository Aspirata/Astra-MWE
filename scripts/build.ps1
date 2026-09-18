param([ValidateSet('amd64','arm64')][string]$Architecture = 'amd64')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Push-Location $projectRoot
$previousArchitecture = $env:GOARCH
try {
    $env:GOARCH = go env GOHOSTARCH
    go run ./cmd/package-icon
    if ($LASTEXITCODE -ne 0) { throw 'Icon generation failed' }
    Push-Location frontend
    try {
        npm ci
        if ($LASTEXITCODE -ne 0) { throw 'Frontend dependency installation failed' }
        npm run build
        if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed' }
    } finally { Pop-Location }
    New-Item -ItemType Directory -Force build/bin | Out-Null
    $env:GOARCH = $Architecture
    go build -trimpath -tags desktop,production -ldflags '-s -w -H windowsgui' -o "build/bin/astra-mwe-windows-$Architecture.exe" .
    if ($LASTEXITCODE -ne 0) { throw 'Desktop build failed' }
    go build -trimpath -ldflags '-s -w' -o "build/bin/astra-cli-windows-$Architecture.exe" ./cmd/astra-cli
    if ($LASTEXITCODE -ne 0) { throw 'CLI build failed' }
} finally { $env:GOARCH = $previousArchitecture; Pop-Location }
