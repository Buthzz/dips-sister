@echo off
setlocal

echo Memulai kompilasi biner distapi...

if not exist dist mkdir dist

if not exist web\dist\index.html (
    where npm >nul 2>nul
    if %errorlevel% equ 0 (
        echo Mengompilasi frontend Vue...
        cd web && npm install && npm run build && cd ..
    ) else (
        echo Error: npm tidak ditemukan dan web\dist\index.html tidak ada.
        exit /b 1
    )
)

echo Kompilasi biner Windows: dist\distapi.exe ...
set CGO_ENABLED=0
set GOOS=windows
set GOARCH=amd64
go build -trimpath -ldflags="-s -w" -o dist\distapi.exe ./cmd/distapi
if %errorlevel% neq 0 (
    echo Error: Gagal kompilasi Windows.
    exit /b %errorlevel%
)

echo Kompilasi biner Linux: dist\distapi ...
set GOOS=linux
set GOARCH=amd64
go build -trimpath -ldflags="-s -w" -o dist\distapi ./cmd/distapi
if %errorlevel% neq 0 (
    echo Error: Gagal kompilasi Linux.
    exit /b %errorlevel%
)

echo.
echo Kompilasi selesai. Berkas biner siap di folder dist\:
dir dist\distapi*
endlocal
