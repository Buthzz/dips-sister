// Package web membungkus aset frontend Vue hasil build Vite agar dapat
// di-embed ke dalam biner distapi (konsep single binary).
package web

import "embed"

// Dist berisi hasil build frontend (index.html + berkas di assets/).
//
// Hasil build di-commit ke repositori di `web/dist/` sehingga seluruh anggota
// cukup menjalankan `go build` tanpa memasang Node.js — konsisten dengan
// kebijakan hasil generate `gen/`.
//
//go:embed all:dist
var Dist embed.FS