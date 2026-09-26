@echo off
setlocal
rem ==============================================================================
rem Script Otomatisasi Kompilasi Biner distapi (Windows CMD / Batch)
rem Sistem Terdistribusi IF2228 - Universitas Trunojoyo Madura
rem ==============================================================================

echo ==========================================================
echo   Membangun Binary distapi (Windows ^& Linux Cross-Build)  
echo ==========================================================

if not exist dist mkdir dist

if not exist web\dist\index.html (
    echo [!] Aset web\dist\index.html belum ditemukan.
    where npm >nul 2>nul
    if %errorlevel% equ 0 (
        echo [*] Mengompilasi frontend Vue dengan npm run build...
        cd web && npm install && npm run build && cd ..
    ) else (
        echo [ERROR] npm tidak ditemukan dan web\dist\index.html tidak ada.
        exit /b 1
    )
) else (
    echo [OK] Aset frontend web\dist siap di-embed ke dalam biner.
)

echo [*] Mengompilasi biner Windows: dist\distapi.exe ...
set CGO_ENABLED=0
set GOOS=windows
set GOARCH=amd64
go build -trimpath -ldflags="-s -w" -o dist\distapi.exe ./cmd/distapi
if %errorlevel% neq 0 (
    echo [ERROR] Gagal kompilasi Windows.
    exit /b %errorlevel%
)
echo     -^> Selesai: dist\distapi.exe (Windows PE32+ 64-bit)

echo [*] Mengompilasi biner Linux: dist\distapi ...
set GOOS=linux
set GOARCH=amd64
go build -trimpath -ldflags="-s -w" -o dist\distapi ./cmd/distapi
if %errorlevel% neq 0 (
    echo [ERROR] Gagal kompilasi Linux.
    exit /b %errorlevel%
)
echo     -^> Selesai: dist\distapi (Linux 64-bit)

echo.
echo ==========================================================
echo [OK] Semua biner berhasil dikompilasi!
echo ==========================================================
dir dist\distapi*
echo ==========================================================
endlocal
