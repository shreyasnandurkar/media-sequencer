# Runs the Go tests.
#
# Why this script exists: on Windows machines with Smart App Control / an
# Application Control policy enabled, `go test` can fail with
#   "An Application Control policy has blocked this file"
# because it compiles a throw-away .exe into a temp folder and immediately
# executes it. The test code is fine - the OS just will not run that binary.
#
# Workaround: compile each test binary to a stable path under .gotest\ and run
# it ourselves. Try plain `go test ./...` first; use this only if it is blocked.
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

$outDir = Join-Path $PSScriptRoot '.gotest'
New-Item -ItemType Directory -Force -Path $outDir | Out-Null

# Packages that actually contain _test.go files.
$packages = go list -f '{{if .TestGoFiles}}{{.ImportPath}}{{end}}' ./... | Where-Object { $_ }

$failed = @()
foreach ($pkg in $packages) {
    $name = ($pkg -split '/')[-1]
    $exe = Join-Path $outDir "$name.test.exe"
    Write-Host "=== $pkg" -ForegroundColor Cyan

    go test -c -o $exe $pkg
    if ($LASTEXITCODE -ne 0) { $failed += $pkg; continue }

    # Test binaries expect the package directory as the working directory
    # (scheduler_test.go reads ..\..\..\testdata by relative path).
    $pkgDir = go list -f '{{.Dir}}' $pkg
    Push-Location $pkgDir
    try {
        & $exe $args
        if ($LASTEXITCODE -ne 0) { $failed += $pkg }
    } finally {
        Pop-Location
    }
}

Remove-Item -Recurse -Force $outDir -ErrorAction SilentlyContinue

if ($failed.Count -gt 0) {
    Write-Host "FAILED: $($failed -join ', ')" -ForegroundColor Red
    exit 1
}
Write-Host "All Go tests passed." -ForegroundColor Green
