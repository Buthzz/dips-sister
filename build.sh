#!/usr/bin/env bash
# ==============================================================================
# Script Otomatisasi Kompilasi Biner distapi (Linux & Windows Cross-Build)
# Sistem Terdistribusi IF2228 - Universitas Trunojoyo Madura
# ==============================================================================

set -e

# Berpindah ke direktori root repository
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$PROJECT_ROOT"

echo "=========================================================="
echo "  Membangun Binary distapi (Windows & Linux Cross-Build)  "
echo "=========================================================="

mkdir -p dist

# 1. Pastikan aset frontend Vue sudah tersedia
if [ ! -f "web/dist/index.html" ]; then
    echo "[!] Aset web/dist/index.html belum ditemukan."
    if command -v npm >/dev/null 2>&1; then
        echo "[*] Mengompilasi frontend Vue dengan npm run build..."
        (cd web && npm install && npm run build)
    else
        echo "[ERROR] npm tidak terpasang di sistem dan web/dist/index.html tidak ditemukan."
        echo "        Pasang Node.js/npm atau salin folder web/dist yang sudah jadi."
        exit 1
    fi
else
    echo "[✓] Aset frontend web/dist siap di-embed ke dalam biner."
fi

# 2. Kompilasi untuk Linux (x86_64 / amd64)
echo "[*] Mengompilasi biner Linux: dist/distapi ..."
rm -f dist/distapi
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/distapi ./cmd/distapi
chmod +x dist/distapi
echo "    -> Selesai: dist/distapi (Linux 64-bit)"

# 3. Kompilasi untuk Windows (x86_64 / amd64)
echo "[*] Mengompilasi biner Windows: dist/distapi.exe ..."
rm -f dist/distapi.exe
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/distapi.exe ./cmd/distapi
echo "    -> Selesai: dist/distapi.exe (Windows PE32+ 64-bit)"

echo ""
echo "=========================================================="
echo "[✓] Semua biner berhasil dikompilasi!"
echo "=========================================================="
ls -lh dist/distapi dist/distapi.exe
echo ""
echo "Catatan Penggunaan:"
echo "  - Di Linux   : ./dist/distapi --mode=master --token=demo123 --tui"
echo "  - Di Windows : .\\dist\\distapi.exe --mode=master --token=demo123 --tui"
echo "=========================================================="
