# Panduan Operasional, REST API, & Troubleshooting — distapi

> **Mata Kuliah:** Sistem Terdistribusi (IF2228)  
> **Lingkungan Pengujian:** Windows 10 / 11 Native (4 Laptop Fisik)  
> **Dokumen Terpadu:** Setup Lingkungan, Eksekusi Kluster, Kontrak REST API, Skenario Demo, & Penanganan Masalah

---

> [!IMPORTANT]
> ### 🚀 Cheatsheet Cepat Hari-H Demo (3 Langkah Anti-Gagal)
> 1. **Laptop 1 (Master):** Buka Windows Settings $\rightarrow$ Aktifkan **Mobile Hotspot**. Buka PowerShell lalu jalankan:
>    ```powershell
>    .\dist\distapi.exe --mode=master --tui
>    ```
> 2. **Laptop 2, 3, 4 (Worker):** Konekkan Wi-Fi ke Hotspot Laptop 1. Buka PowerShell lalu jalankan:
>    ```powershell
>    .\distapi.exe --mode=node --master=<IP_MASTER>:9000 --tui
>    ```
> 3. **Proyektor Kelas:** Buka browser di `http://<IP_MASTER>:8080` untuk mengakses Web UI. Unggah batch gambar dan tunjukkan scatter-gather serta fault tolerance.

---

## 1. Prasyarat & Konfigurasi Lingkungan Windows

### A. Prasyarat Sistem
* **Laptop Utama / Master (Laptop 1):** Kompiler Go versi 1.22 ke atas (unduh di [go.dev/dl](https://go.dev/dl/)) untuk kompilasi binary.
* **Laptop Worker (Laptop 2, 3, 4):** **TIDAK memerlukan Go, Node.js, maupun dependensi eksternal** — cukup salin berkas biner mandiri `distapi.exe`.
* **Terminal:** PowerShell bawaan Windows 10/11.

### B. Konfigurasi Windows Defender Firewall (Sekali Saja di Awal)
Jalankan PowerShell **sebagai Administrator** pada laptop agar port komunikasi masuk (*inbound*) tidak diblokir oleh Windows:

```powershell
# Di SEMUA Laptop (Laptop 1, 2, 3, 4): Izinkan port komunikasi gRPC & RMI
New-NetFirewallRule -DisplayName "distapi gRPC" -Direction Inbound -Protocol TCP -LocalPort 9000 -Action Allow
New-NetFirewallRule -DisplayName "distapi RMI" -Direction Inbound -Protocol TCP -LocalPort 9050 -Action Allow

# Khusus di Laptop 1 (Master / Calon Koordinator Baru): Izinkan port REST API Gateway & Web UI
New-NetFirewallRule -DisplayName "distapi HTTP" -Direction Inbound -Protocol TCP -LocalPort 8080 -Action Allow
```

---

## 2. Kompilasi & Urutan Eksekusi Kluster

### Langkah 1: Kompilasi Binary Mandiri Sekali Jalan
Jalankan script kompilasi otomatis pada folder root project:

```powershell
# Di Windows PowerShell:
.\build.ps1

# (atau di Linux / macOS / Git Bash: ./build.sh, atau via: make build)
```

Script di atas otomatis mengompilasi biner mandiri (*cross-compile*) untuk Windows (`dist\distapi.exe`) dan Linux (`dist/distapi`) dengan seluruh logika backend dan frontend Web Vue 3 ter-embed secara *self-contained* via `//go:embed`. Cukup salin berkas `distapi.exe` via USB Flashdisk ke Laptop 2, 3, dan 4.

### Langkah 2: Jalankan Laptop 1 (Master Node)
Buka PowerShell pada Laptop 1 dan jalankan:

```powershell
.\dist\distapi.exe --mode=master --tui
```

*Terminal Master akan langsung mendeteksi IP fisik Wi-Fi secara otomatis dan menampilkan antarmuka visual:*
* URL Web UI: `http://<IP_MASTER>:8080` (bisa dibuka langsung di browser).
* Perintah instan yang siap disalin untuk Laptop 2, 3, dan 4.

### Langkah 3: Jalankan Laptop 2, 3, 4 (Node Worker)
Seluruh laptop worker dapat mengeksekusi **perintah yang sama persis** tanpa perlu menentukan identitas `--node-id` secara manual:

```powershell
.\dist\distapi.exe --mode=node --master=<IP_MASTER>:9000 --tui
```

*Master secara otomatis mengalokasikan identitas:*
* **Laptop 2** $\rightarrow$ terkonfirmasi sebagai `node-1`.
* **Laptop 3** $\rightarrow$ terkonfirmasi sebagai `node-2`.
* **Laptop 4** $\rightarrow$ terkonfirmasi sebagai `node-3`.
*(Catatan: Token autentikasi default adalah `demo123`. Flag `--token` opsional kecuali diubah).*

### Visual Preview Antarmuka Terminal

#### A. Tampilan Dashboard Laptop 1 (Master — Mode Visual TUI)
```text
 distapi  MASTER   192.168.1.10:9000  (3/3 node aktif)   Web UI: http://192.168.1.10:8080   20:45:10
  Perintah Laptop Worker: .\distapi.exe --mode=node --master=192.168.1.10:9000 --tui

  Node & Job    Log Aktivitas  

Status Node
Node ID        Alamat                  Status       Task Aktif   Kapasitas   Heartbeat       
---------------------------------------------------------------------------------------------
node-1         192.168.1.11:9000       ● alive      0            8           1s lalu         
node-2         192.168.1.12:9000       ● alive      0            8           1s lalu         
node-3         192.168.1.13:9000       ● alive      0            8           2s lalu         

Status Job
Job ID                    Status       Progres      Gagal        Dibuat           
---------------------------------------------------------------------------------------------
job_64fa10b93d8e012a      DONE         12/12        0            25s lalu        

[tab] ganti panel   [↑/↓] navigasi node   [q] keluar
```

#### B. Tampilan Dashboard Laptop 2 (Worker Node-1 — Mode Visual TUI)
```text
 distapi  NODE   node-1   ● Terhubung   20:45:12

Informasi Node
  Node ID:               node-1
  Session:               a4f109bc
  Master:                192.168.1.10:9000
  Advertise:             192.168.1.11:9000
  Heartbeat terakhir:    1s lalu
  Total HB terkirim:     42
  Task aktif / selesai:  0 / 4

Riwayat Pemrosesan Task
  Waktu       Task ID                      Nama Berkas            Durasi       Status     
  ----------------------------------------------------------------------------------------
  20:45:08    job_64fa10b9-000             foto1.jpg              45 ms        ● DONE     
  20:45:09    job_64fa10b9-003             foto4.jpg              52 ms        ● DONE     
  20:45:09    job_64fa10b9-006             foto7.jpg              48 ms        ● DONE     
  20:45:10    job_64fa10b9-009             foto10.jpg             50 ms        ● DONE     

[q] keluar
```

#### C. Tampilan Mode CLI Standar (Tanpa Flag `--tui`)

* **Pada Laptop 1 (Master):**
  ```text
  [distapi Master Aktif]
    IP Master Terdeteksi : 192.168.1.10
    Web UI / REST API    : http://192.168.1.10:8080 (atau http://localhost:8080)
    gRPC Cluster Port    : 192.168.1.10:9000
    RMI net/rpc Port     : 192.168.1.10:9050

  Perintah yang dapat disalin untuk laptop Node Worker:
    .\dist\distapi.exe --mode=node --master=192.168.1.10:9000 --token=demo123 --tui

  2026/09/26 20:45:00 INFO master aktif di 192.168.1.10:9000 (HTTP :8080, RMI :9050)
  2026/09/26 20:45:05 INFO node terdaftar node_id=node-1 addr=192.168.1.11:9000 capacity=8
  2026/09/26 20:45:07 INFO node terdaftar node_id=node-2 addr=192.168.1.12:9000 capacity=8
  2026/09/26 20:45:09 INFO node terdaftar node_id=node-3 addr=192.168.1.13:9000 capacity=8
  ```

* **Pada Laptop 2 (Worker):**
  ```text
  [distapi Node Worker Aktif]
    Node ID              : auto (menunggu alokasi dari master...)
    IP Node Terdeteksi   : 192.168.1.11
    Advertise ke Master  : 192.168.1.11:9000
    Master Tujuan        : 192.168.1.10:9000
    RMI net/rpc Port     : 9050
    Prioritas Pemilihan  : 0 (Bully Algorithm)

  [Node Terdaftar ke Master]
    Identitas Terkonfirmasi : node-1
    Alamat Advertise        : 192.168.1.11:9000
    Master Hubungan         : 192.168.1.10:9000

  2026/09/26 20:45:05 INFO terdaftar ke master node_id=node-1 session=a4f109bc
  2026/09/26 20:45:15 INFO mulai ProcessImage task_id=job_64fa10b9-000 filename=foto1.jpg
  2026/09/26 20:45:15 INFO task selesai duration_ms=45 success=true
  ```

### D. Referensi Lengkap Parameter CLI & Variabel Lingkungan

Seluruh parameter pada biner `distapi` dapat dikonfigurasi melalui flag baris perintah atau environment variable (berguna jika ingin disetel otomatis via skrip):

| Parameter / Flag CLI | Environment Variable | Nilai Default | Kegunaan & Untuk Apa Dipakai |
| :--- | :--- | :--- | :--- |
| `--mode` | `DISTAPI_MODE` | *(Wajib)* | **Menentukan Peran Node:** Nilai `"master"` (koordinator kluster) atau `"node"` (pekerja komputasi pemroses citra). |
| `--master` | `DISTAPI_MASTER` / `DISTAPI_MASTER_ADDR` | *(Wajib di node)* | **Alamat Master Tujuan:** Format `host:port` gRPC master yang dihubungi oleh node worker (contoh: `192.168.1.10:9000`). |
| `--token` | `DISTAPI_TOKEN` | `"demo123"` | **Keamanan Kluster:** Kunci rahasia bersama (*shared secret*) untuk autentikasi gRPC agar node asing tidak bisa sembarangan masuk ke kluster. |
| `--tui` | `DISTAPI_TUI` | `false` | **Tampilan Terminal Interaktif:** Mengaktifkan dashboard visual Bubble Tea (rekomendasi utama saat presentasi demo di kelas). |
| `--node-id` | `DISTAPI_NODE_ID` | `"auto"` | **Identitas Unik Worker:** Mengatur ID node secara manual, atau biarkan `"auto"` agar dialokasikan otomatis oleh Master (`node-1`, `node-2`, dst). |
| `--advertise` | `DISTAPI_ADVERTISE` / `DISTAPI_ADVERTISE_ADDR` | *Auto-detect IP* | **Alamat Balik Node:** Alamat IP & port yang dilaporkan node ke master agar master tahu ke mana task citra harus dikirim melalui gRPC. |
| `--http-port` | `DISTAPI_HTTP_PORT` | `8080` | **Port Antarmuka Pengguna:** Port layanan REST API Gateway dan Web UI pada node Master / Koordinator aktif. |
| `--grpc-port` | `DISTAPI_GRPC_PORT` | `9000` | **Port Komunikasi Kluster:** Port saluran komunikasi biner gRPC berkecepatan tinggi antar-node. |
| `--rmi-port` | `DISTAPI_RMI_PORT` | `9050` | **Port Remote Method Invocation (RMI):** Port server objek terdistribusi `net/rpc` untuk sinkronisasi kluster dan pemilihan koordinator. |
| `--priority` | `DISTAPI_PRIORITY` | `0` *(Otomatis)* | **Bobot Prioritas Pemilihan:** Nilai bobot prioritas Algoritma Bully (Master = 100, node diekstrak dari angka ID, misal: node-1 = 1, node-2 = 2). |
| `--peers` | `DISTAPI_PEERS` | `""` | **Daftar Peer Statik:** Alamat RMI peer node lain (dipisahkan koma, contoh: `192.168.1.11:9050,192.168.1.12:9050`). |
| `--auto-failover`| `DISTAPI_AUTO_FAILOVER` | `true` | **Pemilihan Koordinator Otomatis:** Mengaktifkan deteksi kegagalan koordinator dan inisiasi pemilihan Algoritma Bully jika Master mati. |
| `--workers` | `DISTAPI_WORKERS` | *Jml Core CPU* | **Derajat Paralelisme Lokal:** Jumlah goroutine paralel yang memproses citra secara simultan di dalam satu mesin worker. |
| `--heartbeat-interval`| `DISTAPI_HEARTBEAT_INTERVAL` | `2s` | **Frekuensi Detak Jantung:** Interval pengiriman sinyal detak jantung dari worker ke master (implementasi Failure Detector Cristian 1991). |
| `--node-timeout` | `DISTAPI_NODE_TIMEOUT` | `6s` | **Batas Toleransi Kegagalan Node:** Durasi tanpa detak jantung sebelum master menyatakan worker mati (*dead*) dan mengalihkan task-nya ke worker lain. |
| `--task-timeout` | `DISTAPI_TASK_TIMEOUT` | `30s` | **Batas Waktu Eksekusi Task:** Waktu maksimum untuk memproses satu gambar sebelum dianggap gagal dan dijadwalkan ulang. |
| `--max-retries` | `DISTAPI_MAX_RETRIES` | `3` | **Toleransi Retry Failover:** Jumlah percobaan ulang task jika worker mendadak crash di tengah komputasi (*At-Least-Once Semantics*). |
| `--data-dir` | `DISTAPI_DATA_DIR` | `"./data"` | **Direktori Penyimpanan Berkas:** Lokasi penyimpanan gambar sementara dan hasil transformasi citra di disk lokal. |
| `--max-image-mb` | `DISTAPI_MAX_IMAGE_MB` | `5` | **Batas Ukuran Berkas:** Ukuran maksimum satu file gambar yang diizinkan untuk diunggah (mencegah kehabisan memori RAM). |
| `--max-images` | `DISTAPI_MAX_IMAGES` | `20` | **Batas Batch Gambar:** Jumlah file maksimum dalam satu kali kirim job pemrosesan. |
| `--job-ttl` | `DISTAPI_JOB_TTL` | `1h` | **Masa Simpan Riwayat:** Waktu retensi riwayat job di memori Master sebelum dibersihkan secara otomatis. |
| `--log-level` | `DISTAPI_LOG_LEVEL` | `"info"` | **Kedetilan Catatan Sistem:** Tingkat pencatatan log teks: `debug`, `info`, `warn`, atau `error`. |

#### Perintah Informasi & Bantuan Tambahan

| Perintah / Subcommand | Alias Alternatif | Kegunaan |
| :--- | :--- | :--- |
| `distapi team` | `authors`, `about`, `credits`, `--team` | Menampilkan daftar nama tim pengembang dan NIM tanpa rincian pembagian tugas. |
| `distapi rmi-test [target]` | `test-rmi` | Menguji konektivitas RMI ping dan pemanggilan method remote `ImageProcessor.TransformImage` ke node target (default `127.0.0.1:9050`). |
| `distapi --version` | `version`, `-v` | Menampilkan nomor versi aplikasi saat ini (`distapi v0.1.0`). |
| `distapi -h` | `-help` | Menampilkan ringkasan parameter inti dan sintaks dasar. |
| `distapi --help` | `help` | Menampilkan dokumentasi lengkap, sinkronisasi, detektor kegagalan, RMI, dan mitigasi firewall. |

---

## 3. Spesifikasi Kontrak RESTful API Gateway

Master melayani klien eksternal (browser atau perintah HTTP) melalui 8 endpoint standar:

| Metode | Jalur URI | Deskripsi Operasional | Status Berhasil |
|---|---|---|---|
| `GET` | `/healthz` | Pemeriksaan kesehatan proses Master (*Liveness Probe*) | `200 OK` |
| `POST` | `/api/v1/jobs` | Mengunggah batch gambar baru untuk diproses secara paralel | `202 Accepted` |
| `GET` | `/api/v1/jobs` | Mengambil seluruh daftar riwayat batch job | `200 OK` |
| `GET` | `/api/v1/jobs/{id}` | Mengambil progres real-time dan rincian alokasi task sebuah job | `200 OK` |
| `DELETE` | `/api/v1/jobs/{id}` | Membatalkan job dan menghapus seluruh berkas di storage | `204 No Content` |
| `GET` | `/api/v1/jobs/{id}/results/{file}` | Mengunduh berkas citra hasil olahan secara streaming biner | `200 OK` |
| `GET` | `/api/v1/nodes` | Memeriksa topologi, kapasitas, dan status liveness seluruh node | `200 OK` |
| `GET` | `/api/v1/nodes/{id}/probe` | Menguji konektivitas TCP langsung dari Master ke node tertentu | `200 OK` |

### Contoh Pemanggilan via PowerShell

#### 1. Buat Job Pemrosesan Gambar (Scatter)
```powershell
curl.exe -X POST http://192.168.1.10:8080/api/v1/jobs `
  -F "images=@C:\demo\img1.jpg" `
  -F "images=@C:\demo\img2.png" `
  -F 'options={"grayscale":true,"resize_width":800,"resize_height":800}'
```
*Respons `202 Accepted`:*
```json
{
  "job_id": "job_3f92d81a7b4c2e10",
  "status": "PENDING",
  "tasks": 2
}
```

#### 2. Polling Status Progres Task
```powershell
curl.exe http://192.168.1.10:8080/api/v1/jobs/job_3f92d81a7b4c2e10
```
*Respons `200 OK`:*
```json
{
  "id": "job_3f92d81a7b4c2e10",
  "status": "PROCESSING",
  "total": 2,
  "done": 1,
  "failed": 0,
  "created_at": "2026-09-26T20:15:30.124+07:00",
  "tasks": [
    {
      "id": "job_3f92d81a7b4c2e10-000",
      "filename": "img1.jpg",
      "status": "DONE",
      "assigned_to": "node-1",
      "retries": 0,
      "duration_ms": 42
    },
    {
      "id": "job_3f92d81a7b4c2e10-001",
      "filename": "img2.png",
      "status": "RUNNING",
      "assigned_to": "node-2",
      "retries": 0
    }
  ]
}
```

#### 3. Unduh Hasil Olahan (Gather)
```powershell
Invoke-WebRequest `
  -Uri "http://192.168.1.10:8080/api/v1/jobs/job_3f92d81a7b4c2e10/results/img1.jpg" `
  -OutFile "C:\demo\hasil_img1.jpg"
```

#### 4. Uji Probe TCP ke Node Tertentu
```powershell
curl.exe http://192.168.1.10:8080/api/v1/nodes/node-1/probe
```
*Respons:*
```json
{ "node_id": "node-1", "addr": "192.168.1.11:9000", "reachable": true, "latency_ms": 2 }
```

---

## 4. Skenario Demonstrasi di Depan Kelas

Gunakan urutan demonstrasi praktis 5 menit ini untuk membuktikan fungsionalitas di hadapan dosen:

1. **Topologi Awal:** Buka Web UI Master di browser proyektor (`http://<IP_MASTER>:8080`). Tunjukkan tabel bahwa `node-1`, `node-2`, dan `node-3` telah terhubung dengan status `alive`.
2. **Scatter-Gather Citra:**
   * Unggah 9–12 gambar sekaligus via antarmuka Web UI.
   * Perhatikan kolom *Assigned To* pada tabel: beban tugas terdistribusi merata ke ketiga laptop worker (*Round-Robin*).
3. **Simulasi Kegagalan Worker (Fault Tolerance Task):**
   * Saat proses sedang berjalan, **matikan paksa terminal Laptop 3 (Node 2)** dengan menekan `Ctrl + C`.
   * Perhatikan layar Master: setelah 6 detik, Master mendeteksi detak jantung hilang, status `node-2` berubah menjadi `dead`.
   * Task yang tadinya diemban oleh `node-2` otomatis di-reschedule ke `node-1` atau `node-3`. Seluruh batch job tetap sukses berstatus `DONE`.
4. **Pengujian Mandiri Remote Method Invocation (RMI):**
   * Buka terminal di Laptop 2 (Worker) dan jalankan verifikasi RMI ke Master:
     ```powershell
     .\distapi.exe rmi-test <IP_MASTER>:9050
     ```
   * Tunjukkan kepada dosen: status remote object `Coordinator` dan eksekusi komputasi citra jarak jauh `ImageProcessor.TransformImage` berhasil dieksekusi via protokol RMI.
5. **Simulasi Pergantian Koordinator Otomatis (Bully Algorithm Coordinator Failover):**
   * Tunjukkan ketahanan sistem level tinggi terhadap kegagalan koordinator tunggal: **matikan paksa terminal Laptop 1 (Master)** (`Ctrl + C`).
   * Perhatikan layar Laptop 4 (`node-3` yang memiliki prioritas tertinggi di antara worker aktif): detektor kegagalan mendeteksi koordinator mati (3 kali heartbeat terlewat / 6 detik).
   * `node-3` memicu Algoritma Bully, mendeklarasikan kemenangan, menyiarkan pesan VICTORY ke `node-1` dan `node-2` via RMI, lalu **mempromosikan diri menjadi Koordinator Baru** (mengaktifkan Scheduler dan REST Gateway di port 8080).
   * Laptop 2 (`node-1`) dan Laptop 3 (`node-2`) secara otomatis mengalihkan koneksi gRPC ke Laptop 4 tanpa perlu restart biner.
   * Buka browser proyektor ke alamat Laptop 4 (`http://<IP_LAPTOP_4>:8080`): sistem kluster kembali berjalan normal seutuhnya.

---

## 5. Troubleshooting & Mitigasi Kendala Windows

### Skenario A: Isolasi Klien Wi-Fi Kampus (AP Client Isolation)
* **Gejala:** Laptop Master dan Worker sama-sama terhubung ke Wi-Fi kampus UTM, tetapi perintah ping/gRPC timeout dan worker tidak bisa konek.
* **Akar Masalah:** Router Access Point kampus mengaktifkan fitur *Client Isolation* yang memblokir komunikasi langsung Layer 2 antar laptop.
* **Solusi Anti-Gagal (Rekomendasi Utama Demo):**
  1. Pada Laptop 1 (Master), buka Windows Settings $\rightarrow$ Network & Internet $\rightarrow$ **Mobile Hotspot**, lalu aktifkan.
  2. Hubungkan Laptop 2, 3, dan 4 ke hotspot Laptop Master tersebut.
  3. *Keuntungan:* Bebas isolasi router kampus 100%, latensi transmisi sangat rendah (~1 ms), dan tidak membutuhkan kuota data.

### Skenario B: Windows Defender Firewall Memblokir Port Masuk
* **Gejala:** Master bisa di-ping, tapi koneksi gRPC port 9000 ditolak (*connection refused/timeout*).
* **Solusi:** Jalankan kembali perintah pembukaan port PowerShell Administrator pada Bagian 1.B dokumen ini.

### Skenario C: Konflik Port (*Address Already in Use*)
* **Gejala:** Error `bind: Only one usage of each socket address is normally permitted`.
* **Solusi:** Port 9000 (gRPC), 8080 (HTTP), atau 9050 (RMI) masih terkunci oleh proses lama. Temukan PID dan matikan prosesnya:
  ```powershell
  # Matikan proses yang menduduki port 9000, 8080, atau 9050
  9000, 8080, 9050 | ForEach-Object {
    Get-NetTCPConnection -LocalPort $_ -ErrorAction SilentlyContinue | ForEach-Object { Stop-Process -Id $_.OwningProcess -Force }
  }
  ```

### Skenario D: Skrip Pengecekan Kesehatan Pra-Demo (Pre-Flight Check)
Jalankan skrip diagnostik singkat ini di Laptop Master sebelum memanggil dosen:

```powershell
Write-Host "=== PRE-FLIGHT CHECK DISTAPI ===" -ForegroundColor Cyan

# 1. Cek Kompiler Go
if (Get-Command go -ErrorAction SilentlyContinue) {
    Write-Host "[OK] Go terinstal: $(go version)" -ForegroundColor Green
} else {
    Write-Host "[FAIL] Kompiler Go tidak terdeteksi!" -ForegroundColor Red
}

# 2. Cek Status Port 8080, 9000, & 9050
$p8080 = Get-NetTCPConnection -LocalPort 8080 -ErrorAction SilentlyContinue
$p9000 = Get-NetTCPConnection -LocalPort 9000 -ErrorAction SilentlyContinue
$p9050 = Get-NetTCPConnection -LocalPort 9050 -ErrorAction SilentlyContinue
if ($p8080) { Write-Host "[WARN] Port 8080 terpakai PID $($p8080.OwningProcess)" -ForegroundColor Yellow } else { Write-Host "[OK] Port 8080 bebas." -ForegroundColor Green }
if ($p9000) { Write-Host "[WARN] Port 9000 terpakai PID $($p9000.OwningProcess)" -ForegroundColor Yellow } else { Write-Host "[OK] Port 9000 bebas." -ForegroundColor Green }
if ($p9050) { Write-Host "[WARN] Port 9050 terpakai PID $($p9050.OwningProcess)" -ForegroundColor Yellow } else { Write-Host "[OK] Port 9050 bebas." -ForegroundColor Green }
```
