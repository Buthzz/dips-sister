# ==============================================================================
# Script Otomatisasi Kompilasi Biner distapi (PowerShell - Windows & Linux)
# Sistem Terdistribusi IF2228 - Universitas Trunojoyo Madura
# ==============================================================================

$ErrorActionPreference = "Stop"

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "  Membangun Binary distapi (Windows & Linux Cross-Build)  " -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan

if (-not (Test-Path -Path "dist")) {
    New-Item -ItemType Directory -Path "dist" | Out-Null
}

# 1. Pastikan aset frontend Vue sudah tersedia
if (-not (Test-Path -Path "web/dist/index.html")) {
    Write-Host "[!] Aset web/dist/index.html belum ditemukan." -ForegroundColor Yellow
    if (Get-Command npm -ErrorAction SilentlyContinue) {
        Write-Host "[*] Mengompilasi frontend Vue dengan npm run build..." -ForegroundColor Yellow
        Push-Location web
        npm install
        npm run build
        Pop-Location
    } else {
        Write-Error "npm tidak ditemukan dan web/dist/index.html tidak ada. Pasang Node.js/npm terlebih dahulu."
        exit 1
    }
} else {
    Write-Host "[✓] Aset frontend web/dist siap di-embed ke dalam biner." -ForegroundColor Green
}

# 2. Kompilasi untuk Windows (x86_64 / amd64)
Write-Host "[*] Mengompilasi biner Windows: dist/distapi.exe ..." -ForegroundColor Yellow
$env:CGO_ENABLED = "0"
$env:GOOS = "windows"
$env:GOARCH = "amd64"
go build -trimpath -ldflags="-s -w" -o dist/distapi.exe ./cmd/distapi
Write-Host "    -> Selesai: dist/distapi.exe (Windows PE32+ 64-bit)" -ForegroundColor Green

# 3. Kompilasi untuk Linux (x86_64 / amd64)
Write-Host "[*] Mengompilasi biner Linux: dist/distapi ..." -ForegroundColor Yellow
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -trimpath -ldflags="-s -w" -o dist/distapi ./cmd/distapi
Write-Host "    -> Selesai: dist/distapi (Linux 64-bit)" -ForegroundColor Green

# Reset variabel environment
$env:GOOS = ""
$env:GOARCH = ""
$env:CGO_ENABLED = ""

Write-Host ""
Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "[✓] Semua biner berhasil dikompilasi!" -ForegroundColor Green
Write-Host "==========================================================" -ForegroundColor Cyan
Get-ChildItem -Path "dist/distapi*" | Where-Object { -not $_.PSIsContainer } | Select-Object Name, Length, LastWriteTime | Format-Table -AutoSize
