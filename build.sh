#!/usr/bin/env bash
# Script kompilasi biner distapi (Linux & Windows)
set -e

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$PROJECT_ROOT"

echo "Memulai kompilasi biner distapi..."

mkdir -p dist

# 1. Pastikan aset frontend Vue sudah tersedia
if [ ! -f "web/dist/index.html" ]; then
    if command -v npm >/dev/null 2>&1; then
        echo "Mengompilasi frontend Vue..."
        (cd web && npm install && npm run build)
    else
        echo "Error: npm tidak terpasang dan web/dist/index.html tidak ditemukan." >&2
        exit 1
    fi
fi

# 2. Kompilasi untuk Linux (amd64)
echo "Kompilasi biner Linux: dist/distapi ..."
rm -f dist/distapi
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/distapi ./cmd/distapi
chmod +x dist/distapi

# 3. Kompilasi untuk Windows (amd64)
echo "Kompilasi biner Windows: dist/distapi.exe ..."
rm -f dist/distapi.exe
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/distapi.exe ./cmd/distapi

echo ""
echo "Kompilasi selesai. Berkas biner siap di folder dist/:"
ls -lh dist/distapi dist/distapi.exe
