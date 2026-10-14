$ErrorActionPreference = 'Stop'
$previousGoOS = $env:GOOS
$previousGoArch = $env:GOARCH
$previousCGO = $env:CGO_ENABLED
try {
    $env:CGO_ENABLED = '0'
    foreach ($platform in @('windows', 'linux')) {
        foreach ($architecture in @('amd64', 'arm64')) {
            $env:GOOS = $platform
            $env:GOARCH = $architecture
            $outputDirectory = Join-Path 'build' "$platform-$architecture"
            New-Item -ItemType Directory -Path $outputDirectory -Force | Out-Null
            $filename = if ($platform -eq 'windows') { 'PingObservatory.exe' } else { 'PingObservatory' }
            & go build -trimpath -ldflags '-s -w' -o (Join-Path $outputDirectory $filename) ./cmd/pingobservatory
            if ($LASTEXITCODE -ne 0) { throw "Build failed for $platform/$architecture" }
        }
    }
} finally {
    $env:GOOS = $previousGoOS
    $env:GOARCH = $previousGoArch
    $env:CGO_ENABLED = $previousCGO
}
