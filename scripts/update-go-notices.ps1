$ErrorActionPreference = 'Stop'
$noticeDirectory = Join-Path $PSScriptRoot '..\internal\legal\notices'
$previousGoOS = $env:GOOS
$previousGoArch = $env:GOARCH
$previousCGO = $env:CGO_ENABLED
$allModuleRows = [System.Collections.Generic.List[string]]::new()
try {
    $env:CGO_ENABLED = '0'
    foreach ($platform in @('windows', 'linux')) {
        foreach ($architecture in @('amd64', 'arm64')) {
            $env:GOOS = $platform
            $env:GOARCH = $architecture
            $rows = @(& go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}}|{{.Version}}|{{.Dir}}{{end}}{{end}}' ./cmd/pingobservatory)
            if ($LASTEXITCODE -ne 0) { throw "Could not inspect dependency modules for $platform/$architecture." }
            foreach ($row in $rows) { $allModuleRows.Add($row) }
        }
    }
} finally {
    $env:GOOS = $previousGoOS
    $env:GOARCH = $previousGoArch
    $env:CGO_ENABLED = $previousCGO
}
$moduleRows = @($allModuleRows | Sort-Object -Unique)
$parts = [System.Collections.Generic.List[string]]::new()
foreach ($row in $moduleRows) {
    if ([string]::IsNullOrWhiteSpace($row)) { continue }
    $fields = $row.Split('|', 3)
    if ($fields.Count -ne 3 -or [string]::IsNullOrWhiteSpace($fields[2])) { throw 'Dependency module directory is unavailable.' }
    $files = @(Get-ChildItem -LiteralPath $fields[2] -File | Where-Object { $_.Name -match '^(LICENSE|COPYING|NOTICE)([._-].*)?$' } | Sort-Object Name)
    if ($files.Count -eq 0) { throw "Dependency licence is missing: $($fields[0])" }
    $parts.Add("$($fields[0]) $($fields[1])")
    foreach ($file in $files) {
        $parts.Add($file.Name)
        $parts.Add([System.IO.File]::ReadAllText($file.FullName))
    }
}
$text = ($parts -join "`n`n").TrimEnd() + "`n"
[System.IO.File]::WriteAllText((Join-Path $noticeDirectory 'go-dependencies.txt'), $text, [System.Text.UTF8Encoding]::new($false))
