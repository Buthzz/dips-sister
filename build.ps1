# Script kompilasi biner distapi (PowerShell - Windows & Linux)
$ErrorActionPreference = "Stop"

Write-Host "Memulai kompilasi biner distapi..." -ForegroundColor Cyan

if (-not (Test-Path -Path "dist")) {
    New-Item -ItemType Directory -Path "dist" | Out-Null
}

# 1. Pastikan aset frontend Vue sudah tersedia
if (-not (Test-Path -Path "web/dist/index.html")) {
    if (Get-Command npm -ErrorAction SilentlyContinue) {
        Write-Host "Mengompilasi frontend Vue..." -ForegroundColor Yellow
        Push-Location web
        npm install
        npm run build
        Pop-Location
    } else {
        Write-Error "npm tidak ditemukan dan web/dist/index.html tidak ada."
        exit 1
    }
}

# 2. Kompilasi untuk Windows (amd64)
Write-Host "Kompilasi biner Windows: dist/distapi.exe ..." -ForegroundColor Yellow
$env:CGO_ENABLED = "0"
$env:GOOS = "windows"
$env:GOARCH = "amd64"
go build -trimpath -ldflags="-s -w" -o dist/distapi.exe ./cmd/distapi

# 3. Kompilasi untuk Linux (amd64)
Write-Host "Kompilasi biner Linux: dist/distapi ..." -ForegroundColor Yellow
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -trimpath -ldflags="-s -w" -o dist/distapi ./cmd/distapi

# Reset environment variables
$env:GOOS = ""
$env:GOARCH = ""
$env:CGO_ENABLED = ""

Write-Host ""
Write-Host "Kompilasi selesai. Berkas biner siap di folder dist/:" -ForegroundColor Green
Get-ChildItem -Path "dist/distapi*" | Where-Object { -not $_.PSIsContainer } | Select-Object Name, Length, LastWriteTime | Format-Table -AutoSize
