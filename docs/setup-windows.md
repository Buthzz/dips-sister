# Setup di Windows — distapi

## Prasyarat

| Kebutuhan | Versi Minimum | Link |
|-----------|---------------|------|
| Go | 1.22 (direkomendasikan 1.26+) | https://go.dev/dl/ |
| Git | Sembarang | https://git-scm.com/ |
| PowerShell | Sudah ada di Windows 10+ | — |

Node **tidak membutuhkan Go** — cukup copy file `distapi.exe` ke laptop node.

## Langkah Instalasi

### 1. Install Go (semua laptop yang perlu build)

1. Download installer dari https://go.dev/dl/ (pilih `go1.26.x.windows-amd64.msi`)
2. Jalankan installer, ikuti wizard
3. Buka PowerShell baru dan verifikasi:
   ```powershell
   go version
   # Output: go version go1.26.x windows/amd64
   ```

### 2. Clone repository

```powershell
git clone <url-repo> distapi
cd distapi
```

### 3. Build binary

```powershell
go build -o dist\distapi.exe .\cmd\distapi
# Hasilnya: dist\distapi.exe (single file, tidak ada dependency eksternal)
```

### 4. Distribusi ke laptop lain

Cukup copy `dist\distapi.exe` ke laptop node. Tidak perlu install Go di laptop node.

## Konfigurasi Jaringan

### Cek IP Laptop

```powershell
ipconfig
# Cari "IPv4 Address" pada adapter WiFi atau Ethernet
# Contoh: 192.168.1.10
```

### Izinkan Port di Windows Firewall

Jalankan PowerShell **sebagai Administrator** di **semua laptop**:

```powershell
# Izinkan port gRPC (9000)
New-NetFirewallRule -DisplayName "distapi gRPC" `
  -Direction Inbound -Protocol TCP -LocalPort 9000 -Action Allow

# Izinkan port HTTP (8080, hanya di master)
New-NetFirewallRule -DisplayName "distapi HTTP" `
  -Direction Inbound -Protocol TCP -LocalPort 8080 -Action Allow
```

### Tes Konektivitas Antar Laptop

Sebelum menjalankan distapi, pastikan laptop bisa saling berkomunikasi:

```powershell
# Dari laptop node, ping master
ping 192.168.1.10

# Dari laptop master, test TCP ke node (setelah distapi.exe dijalankan di node)
Test-NetConnection -ComputerName 192.168.1.11 -Port 9000
# Status: TcpTestSucceeded : True
```

> Jika WiFi kampus menerapkan **client isolation** (laptop tidak bisa saling ping), gunakan hotspot dari HP sebagai access point.

## Menjalankan Sistem

### Urutan Startup (Penting!)

1. **Laptop 1 — Master** (jalankan pertama)
2. **Laptop 2, 3, 4 — Node** (bisa bersamaan, setelah master ready)

### Master

```powershell
.\dist\distapi.exe `
  --mode=master `
  --http-port=8080 `
  --grpc-port=9000 `
  --token=demo123 `
  --log-level=info `
  --data-dir=.\data
```

Tunggu sampai log menunjukkan: `gRPC server mendengarkan addr=:9000`

### Node (tiap laptop)

```powershell
.\dist\distapi.exe `
  --mode=node `
  --node-id=node-1 `
  --master=192.168.1.10:9000 `
  --advertise=192.168.1.11:9000 `
  --grpc-port=9000 `
  --token=demo123 `
  --log-level=info
```

Ganti `node-1` → `node-2`, `node-3` untuk laptop berikutnya.
Ganti `192.168.1.11` dengan IP setiap laptop node.

## Verifikasi Sistem Berjalan

```powershell
# Dari master atau laptop lain di jaringan yang sama
curl http://192.168.1.10:8080/healthz
# Response: {"status":"ok"}

curl http://192.168.1.10:8080/api/v1/nodes
# Response: [{"node_id":"node-1","status":"alive",...}, ...]
```

## Troubleshooting

### Node tidak bisa connect ke master

```
PROBLEM: "tidak bisa connect ke master"
SOLUSI:
1. Pastikan master sudah jalan dulu
2. Verifikasi IP master sudah benar di --master flag
3. Test: Test-NetConnection -ComputerName <IP_MASTER> -Port 9000
4. Cek firewall: rule "distapi gRPC" sudah ada?
```

### Heartbeat ditolak

```
PROBLEM: log "heartbeat ditolak" di master
SOLUSI: master akan meminta node register ulang (ok, ini normal setelah master restart)
```

### "Access Denied" saat menulis file

```
PROBLEM: storage: rename gagal: Access is denied
SOLUSI: Jalankan distapi.exe dari direktori yang kamu punya hak tulis
        Hindari C:\Windows atau C:\Program Files
        Gunakan --data-dir=.\data (relatif dari posisi .exe)
```

### Port sudah dipakai

```
PROBLEM: "listen :9000 gagal: bind: Only one usage..."
SOLUSI:
1. Cari proses yang pakai port: netstat -ano | findstr ":9000"
2. Kill proses: taskkill /PID <PID> /F
3. Atau gunakan port lain: --grpc-port=9001
```

## Menjalankan Unit Test

```powershell
# Jalankan semua test
go test ./...

# Verbose (lihat nama setiap test)
go test -v ./...

# Hanya satu package
go test -v ./internal/scheduler/...

# Analisis statik
go vet ./...
```
