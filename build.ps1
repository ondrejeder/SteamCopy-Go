param([string]$Go = 'go')
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot
if (-not (Get-Command $Go -ErrorAction SilentlyContinue)) {
    $fallback = 'C:\Coding\SaveSync Ally\tools\go1.26.2\bin\go.exe'
    if (-not (Test-Path -LiteralPath $fallback)) { throw 'Go 1.25 or newer is required. Add go.exe to PATH or pass -Go with its full path.' }
    $Go = $fallback
}
$saved = @{}
foreach ($name in @('GOOS','GOARCH','CGO_ENABLED')) { $saved[$name] = [Environment]::GetEnvironmentVariable($name,'Process') }
try {
    $env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
    & $Go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'Tests failed.' }
    New-Item -ItemType Directory -Force -Path dist | Out-Null
    & $Go build -trimpath -o dist/steamcopy-windows-amd64.exe .
    if ($LASTEXITCODE -ne 0) { throw 'Windows build failed.' }
    $env:GOOS = 'linux'; $env:GOARCH = 'arm64'
    & $Go build -trimpath -o dist/steamcopy-linux-arm64 .
    if ($LASTEXITCODE -ne 0) { throw 'Linux ARM64 build failed.' }
    Copy-Item -LiteralPath Start-SteamCopy.sh -Destination dist/Start-SteamCopy.sh -Force
    $checksums = foreach ($file in @('dist/steamcopy-windows-amd64.exe','dist/steamcopy-linux-arm64','dist/Start-SteamCopy.sh')) {
        '{0}  {1}' -f (Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash.ToLowerInvariant(), (Split-Path -Leaf $file)
    }
    $checksums | Set-Content -LiteralPath dist/SHA256SUMS -Encoding ascii
} finally { foreach ($name in $saved.Keys) { [Environment]::SetEnvironmentVariable($name,$saved[$name],'Process') } }
