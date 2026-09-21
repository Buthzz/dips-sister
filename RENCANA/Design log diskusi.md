# Distributed Image Processing Service — Dokumen Desain

> Tugas kelompok mata kuliah **Sistem Terdistribusi**
> Tim: 4 orang · Perangkat: 4 laptop (1 master + 3 node)
> Status dokumen: **DRAFT — keputusan default sudah ada (Bagian 13), poin yang masih menunggu verifikasi: D1, D2, D3, D16**

---

## 1. Ringkasan

Aplikasi web tempat pengguna mengunggah beberapa gambar. Sistem membagi gambar
ke **4 laptop** untuk diproses secara paralel (resize + grayscale), lalu hasilnya
ditampilkan dan bisa diunduh.

**Ketentuan tugas:** mendemokan *aplikasi web dengan arsitektur RESTful API*.
**Ketentuan tambahan dari tim:** RPC wajib muncul, Go untuk backend (1 master,
sisanya node), Vue.js untuk frontend, best practices industri tetapi tidak
over-engineering.

**Prinsip yang dipegang:**

1. Stdlib dulu; library eksternal hanya jika menghapus risiko bug atau
   boilerplate yang nyata.
2. Ambil praktik industri yang murah dan bernilai; lewati yang hanya relevan
   pada skala produksi besar.
3. Setiap keputusan harus bisa dijelaskan alasannya saat demo dan di laporan.

---

## 2. Cakupan (Scope)

### Termasuk

- Unggah beberapa gambar lewat halaman web
- Pembagian task ke 4 laptop (master ikut bekerja)
- Pemrosesan: resize + grayscale
- Progres job dan status node secara langsung di UI
- Deteksi node mati dan penjadwalan ulang task otomatis
- Unduh hasil

### Tidak termasuk (sengaja dilewati)

Database, autentikasi pengguna, TLS/mTLS, Docker/Kubernetes, metrics
Prometheus, distributed tracing, circuit breaker, leader election, message
queue, Vue Router, Pinia/Vuex.

> Semua di atas dicatat sebagai *future work* di laporan. **Scope dibekukan**;
> tambahan fitur apa pun ditolak sampai tugas selesai.

---

## 3. Arsitektur

### 3.1 Pola: Master–Worker dengan Scatter–Gather

```
                       ┌────────────────────┐
                       │  Browser (Vue.js)  │
                       └─────────┬──────────┘
                                 │ REST / JSON  (HTTP)
                                 ▼
                    ┌─────────────────────────┐
                    │   LAPTOP 1 — MASTER     │
                    │  • REST API + static UI │
                    │  • Scheduler            │
                    │  • Registry + heartbeat │
                    │  • Worker lokal         │
                    └───┬─────────┬─────────┬─┘
                gRPC    │         │         │    gRPC
              ┌─────────┘         │         └─────────┐
              ▼                   ▼                   ▼
      ┌──────────────┐    ┌──────────────┐    ┌──────────────┐
      │ LAPTOP 2     │    │ LAPTOP 3     │    │ LAPTOP 4     │
      │ NODE-1       │    │ NODE-2       │    │ NODE-3       │
      │ gRPC server  │    │ gRPC server  │    │ gRPC server  │
      │ + worker     │    │ + worker     │    │ + worker     │
      └──────────────┘    └──────────────┘    └──────────────┘
```

### 3.2 Istilah yang dipakai di laporan

| Istilah | Pemetaan |
|---|---|
| **Control plane** | Master: API, scheduler, registry, deteksi kegagalan |
| **Data plane** | Worker di keempat laptop: memproses gambar |
| **Scatter–gather** | Master memecah job jadi task, menyebar, lalu mengumpulkan hasil |

### 3.2a Pemetaan ke materi kuliah (Pertemuan 4: Arsitektur)

Slide kuliah menyebut dua arsitektur utama: **Master-Slave** (peran entitas
*asimetris*) dan **Peer-to-Peer** (peran *simetris*). Proyek ini termasuk
**Master-Slave**; istilah "master-worker" di dokumen ini adalah nama lain untuk
pola yang sama (slide memakai istilah *Worker* pada gambarnya).

| Peran master menurut slide | Di proyek ini |
|---|---|
| Menangani request klien | REST API (`/api/v1/jobs`) |
| Memantau resource sistem | Registry + heartbeat node |
| Mengelola resource dan menjadwalkan job ke slave | Scheduler (satu gambar = satu task) |
| Melacak progres job di slave | Status task `PENDING → RUNNING → DONE / FAILED` |
| Mengagregasi hasil dari slave | Scatter–gather di master |
| Melapor ke klien | Polling `GET /api/v1/jobs/{id}` dari UI |

Slide juga menyebut tiga karakteristik Master-Slave, dan proyek ini
**mengakui ketiganya** (bukan menyembunyikannya):

| Karakteristik menurut slide | Kenapa berlaku di sini |
|---|---|
| Node tidak setara → rentan *Single-Point-of-Failure* | Master mati, sistem berhenti (lihat 3.5) |
| Master sebagai *central coordinator* → keputusan mudah | Scheduling terpusat, tanpa konsensus |
| Tidak bisa *scale out* tanpa batas → master jadi *bottleneck* | Master menerima upload, menyimpan hasil, dan mengirim semua task |

> Alasan tidak memilih Peer-to-Peer: slide menyebut P2P membuat *decision
> making becomes harder*. Untuk tugas ini, pengambilan keputusan terpusat lebih
> sederhana dan lebih mudah didemokan.

### 3.3 Ini bukan microservice

| Aspek | Microservice | Sistem ini |
|---|---|---|
| Dipecah berdasarkan | Fungsi bisnis | Beban kerja |
| Kode tiap instance | Berbeda | **Sama** (single binary) |
| Tujuan | Modularitas, deploy independen | Paralelisme dan performa |
| Komunikasi | Antar-service saling memanggil | Bintang: master ke node |

Jawaban singkat jika ditanya dosen: *"Bukan. Microservice memecah sistem
berdasarkan fungsi bisnis dengan kode berbeda per service. Sistem kami memecah
berdasarkan beban kerja dengan kode yang sama di semua node, untuk paralelisme.
Polanya master-worker dengan scatter-gather."*

### 3.4 Prinsip desain sistem terdistribusi yang diterapkan

| Prinsip | Implementasi |
|---|---|
| **Stateless worker** | Node tidak menyimpan state penting; bisa mati/muncul tanpa migrasi data |
| **Idempotent task** | Task yang sama boleh berjalan dua kali dengan hasil sama (aman untuk retry) |
| **At-least-once execution** | Jaminan yang jujur; duplikasi dimungkinkan, dampaknya diminimalkan lewat idempotent processing dan *first-result-wins* |
| **Failure detection** | Heartbeat berkala; node dianggap mati jika 3 interval terlewat |
| **Rescheduling** | Task node yang mati dialihkan ke node lain |
| **Timeout di semua panggilan jaringan** | Jaringan tidak bisa dipercaya |
| **Transparansi** | Klien hanya berbicara ke satu endpoint (master) |

### 3.5 Keterbatasan yang diakui (bahan diskusi laporan)

- **Master adalah Single Point of Failure.** Jika master mati, sistem berhenti
  dan job yang berjalan hilang. Produksi mengatasinya dengan replikasi master
  dan konsensus (Raft/Paxos); di luar scope tugas.
- **State job disimpan di memori master.** Restart master menghapus riwayat.
- **Kegagalan RPC ≠ worker mati.** Worker bisa hanya tidak terjangkau tetapi
  tetap menghitung, sehingga dua worker bisa mengerjakan task yang sama.
  Karena itu task harus idempotent.

---

## 4. Layering (organisasi vertikal layanan)

Menurut slide kuliah, **layering = organisasi vertikal layanan**: sistem
kompleks dipartisi menjadi lapisan, lapisan atas memakai layanan lapisan di
bawahnya, dan kompleksitas lapisan bawah disembunyikan.

### 4.1 Layering tingkat sistem (sesuai slide: tiga layer besar)

| Layer (slide) | Di proyek ini |
|---|---|
| **Applications** | Scheduler, worker, REST API, aplikasi Vue |
| **Middleware** | **gRPC + Protocol Buffers** — menyembunyikan detail jaringan dan *marshalling*, sehingga scheduler cukup memanggil `ProcessImage(...)` seperti fungsi biasa |
| **Platform** | OS dan perangkat keras laptop (jaringan WiFi/LAN) |

Ini juga menjawab pertanyaan "di mana RPC pada arsitektur kalian?": RPC berada
di lapisan **middleware**.

### 4.2 Layering di dalam kode (turunan dari layer *Applications*)

Lapisan atas hanya boleh memanggil lapisan di bawahnya.

```
┌─────────────────────────────────────────────┐
│ 1. API Layer                                │  handler REST, validasi input,
│    internal/api                             │  pemetaan error → status HTTP
├─────────────────────────────────────────────┤
│ 2. Coordination Layer                       │  pecah job, jadwalkan, gabungkan
│    internal/scheduler                       │  hasil, retry/reschedule
├─────────────────────────────────────────────┤
│ 3. Communication Layer                      │  klien & server gRPC,
│    internal/rpc                             │  timeout, metadata token
├─────────────────────────────────────────────┤
│ 4. State / Infrastructure Layer             │  registry node + heartbeat,
│    internal/registry, internal/storage      │  penyimpanan file sementara
└─────────────────────────────────────────────┘
        internal/worker  → logika komputasi murni (dipakai master & node)
```

**Praktik layering yang diterapkan:**

- Kontrak antar-lapisan berupa **tipe eksplisit**, bukan `map[string]any`.
- Detail transport (status code HTTP, tipe error gRPC) **tidak bocor** ke
  lapisan koordinasi.
- Error diterjemahkan di batas lapisan (handler yang memetakan ke status HTTP).
- Satu unsur *hexagonal*: scheduler bergantung pada interface kecil
  `NodeClient`, sehingga bisa dites dengan *fake* tanpa jaringan.

```go
// Contoh bentuk interface (indikatif, bukan final)
type NodeClient interface {
    ProcessImage(ctx context.Context, nodeID string, task Task) (Result, error)
}
```

> Menurut slide: **tiering = pemisahan horizontal layanan**, **layering =
> organisasi vertikal layanan**; keduanya saling melengkapi. Cara praktis
> membedakannya di proyek ini: layering menentukan *siapa memanggil siapa* di
> dalam kode; tiering menentukan *komponen mana berjalan di mesin mana*. Satu
> tier bisa berisi beberapa layer.

---

## 5. Tiering (pemisahan horizontal layanan)

Menurut slide, tiering adalah teknik untuk (1) mengorganisasi fungsi sebuah
layanan, dan (2) menempatkan fungsi itu ke server yang sesuai. Slide memberi
contoh **tiga tier**: *Presentation Logic*, *Application Logic*, *Data Logic*.

### 5.1 Pemetaan ke contoh slide

| Tier (slide) | Di proyek ini | Mesin |
|---|---|---|
| **Tier 1 — Presentation Logic** | Aplikasi Vue di browser | Laptop mana saja |
| **Tier 2 — Application Logic** | REST API, scheduler, registry | Laptop 1 (master) |
| **Tier 3 — Data Logic** | **Diganti Processing tier**: worker pemroses gambar | Laptop 1–4 (master ikut) |

Catatan jujur tentang Tier 3: pada contoh slide, tier ini adalah *database
manager*. Proyek ini **tidak memiliki database**, sehingga tier ketiga
digantikan tier pemrosesan. Ini bukan tiga tier klasik persis, melainkan
**variasi tiga tier** dan sebaiknya disebut demikian saat presentasi.

### 5.2 Kelebihan dan kekurangan (menurut slide, berlaku juga di sini)

| Kelebihan | Kekurangan |
|---|---|
| Pemetaan satu-satu dari elemen logis ke server fisik → mudah dirawat | Kompleksitas mengelola banyak server |
| Setiap tier punya peran yang jelas | Tambahan trafik jaringan |
| | Tambahan latensi (dibuktikan lewat benchmark) |

### 5.3 Catatan tentang peran ganda master dan ketiadaan data tier

- Master menjalankan **dua peran di satu mesin** (coordination + processing);
  di kode, itu worker lokal terpisah dari scheduler.
- **Tidak ada data tier** karena sistem stateless untuk komputasi. Jika
  ditanya: menambahkannya tanpa kebutuhan nyata melanggar prinsip
  non-over-engineering. Persistence bisa menjadi tier ke-4 di pengembangan
  lanjutan.

---

## 6. Komunikasi

**Pola: REST di tepi, gRPC di dalam** (konsisten di banyak sumber industri).

| Jalur | Protokol | Alasan |
|---|---|---|
| Browser → Master | **REST/JSON** | Browser tidak bisa gRPC native (butuh gRPC-Web); tugas mensyaratkan RESTful |
| Master ↔ Node | **gRPC (protobuf)** | Kontrak ketat, efisien untuk data biner (gambar), RPC wajib muncul |

**Jawaban untuk laporan:** antarmuka publik = RESTful API; antarmuka internal
cluster = gRPC (RPC). Keduanya muncul beserta alasannya.

### 6.1 REST API (publik)

| Method | Endpoint | Fungsi | Status |
|---|---|---|---|
| POST | `/api/v1/jobs` | Buat job (upload gambar) | 202 Accepted |
| GET | `/api/v1/jobs` | Daftar job | 200 |
| GET | `/api/v1/jobs/{id}` | Detail dan progres job | 200 / 404 |
| GET | `/api/v1/jobs/{id}/results/{file}` | Unduh hasil | 200 / 404 |
| DELETE | `/api/v1/jobs/{id}` | Batalkan/hapus job | 204 / 404 |
| GET | `/api/v1/nodes` | Status node cluster | 200 |
| GET | `/healthz` | Health check | 200 |

**Ciri RESTful yang dipenuhi:** resource berupa kata benda, method HTTP sesuai
makna, status code yang benar, stateless, format error seragam, versioning
`/api/v1`.

**Pola asynchronous request–reply:** `POST /jobs` langsung membalas `202` dengan
ID job; UI memantau lewat `GET /jobs/{id}` (polling) sampai selesai. Koneksi
HTTP tidak ditahan selama komputasi.

### 6.2 gRPC (internal) — sketsa kontrak

Kontrak dipecah menjadi **dua service** karena arah panggilannya berbeda:

```proto
// Berjalan di MASTER; dipanggil oleh NODE
service Coordinator {
  rpc Register(RegisterRequest)   returns (RegisterResponse);
  rpc Heartbeat(HeartbeatRequest) returns (HeartbeatResponse);
}

// Berjalan di setiap NODE (dan master untuk worker lokal, tanpa lewat jaringan);
// dipanggil oleh MASTER
service Worker {
  rpc ProcessImage(ProcessRequest) returns (ProcessResponse);
}
```

```
NODE   ──Register / Heartbeat──►  MASTER   (service Coordinator)
MASTER ──ProcessImage──────────►  NODE     (service Worker)
```

Master dan node sama-sama menjalankan **gRPC server** dan **gRPC client**:
master menjadi server `Coordinator` dan client `Worker`; node menjadi server
`Worker` dan client `Coordinator`. Pemisahan ini juga memudahkan pembagian kerja
(anggota B).

> **Keputusan default (lihat Bagian 13):** heartbeat dan registrasi dimulai node
> (node → master); task dikirim master ke node (push); gambar dikirim **unary**
> dengan batas 5 MB (batas pesan gRPC dinaikkan eksplisit ke ~8 MB, karena
> default 4 MB). Detail kontrak final ditulis di langkah berikutnya.

---

## 7. Tech Stack

| Lapisan | Teknologi | Versi |
|---|---|---|
| Backend | Go | lantai **`go 1.25.0`** di `go.mod`; dipasang **Go 1.26.x** |
| RPC internal | gRPC + Protocol Buffers | gRPC-Go **≥ v1.79.3** |
| REST | `net/http` (routing Go 1.22+) | stdlib |
| Frontend | Vue 3 + Vite (Composition API) | — |
| Build frontend | Node.js + npm | 18+ (hanya di mesin yang build) |
| Embed frontend | `//go:embed` | stdlib |
| Build & tugas | Makefile | — |
| CI (opsional) | GitHub Actions: test + build | — |

**Kebijakan versi Go:**

| Hal | Keputusan | Alasan (terverifikasi dari sumber resmi) |
|---|---|---|
| Baris `go` di `go.mod` | `go 1.25.0` | Catatan rilis gRPC-Go v1.81.0 menyatakan Go minimum yang didukung adalah 1.25. Baris ini adalah **minimum**, bukan target |
| Versi yang dipasang tim | **Go 1.26.x** | Didukung resmi oleh Go dan gRPC-Go (keduanya mendukung dua rilis mayor terbaru); sudah melewati banyak rilis patch |
| Go 1.27 | Boleh, bukan pilihan pertama | Baru rilis 19 Agustus 2026; patch pertamanya (1.27.1) baru terbit 1 September dan memuat perbaikan di `net/http` dan `encoding/json` yang kita pakai |
| Go 1.25 | Cukup untuk *membangun*, tidak disarankan dipasang | Secara formal sudah di luar dukungan Go setelah 1.27 rilis |

> **Peringatan:** lantai `1.25` **bisa naik** di rilis gRPC-Go berikutnya.
> Alasannya dijelaskan tim gRPC sendiri: baris `go` di `go.mod` adalah versi
> minimum yang wajib, sehingga setiap dependensi yang mensyaratkan Go lebih
> baru memaksa lantai naik. Jangan mengunci versi lama sambil berharap gRPC ikut
> diam.

**Kenapa gRPC ≥ v1.79.3:** CVE-2026-33186 (bypass otorisasi pada validasi
header `:path` HTTP/2) diperbaiki di versi tersebut. Celah ini spesifik pada
interceptor otorisasi berbasis path, sedangkan proyek ini hanya memakai token
sederhana, tetapi versi aman tetap dipakai. Rilis terbaru saat dokumen ini
ditulis: **v1.83.2** (25 Agustus 2026).

**Cara memverifikasi lantai versi (jalankan sekali saat repo dibuat):**

```bash
go mod init distapi
go get google.golang.org/grpc@latest \
       google.golang.org/protobuf@latest \
       golang.org/x/sync@latest
go mod tidy
grep -E '^(go|toolchain) ' go.mod
```

Jika Go menaikkan baris `go` di atas `1.25.0`, itu **bukti langsung** bahwa
salah satu dependensi mensyaratkan versi lebih baru, dan perintah `go mod tidy`
menunjukkan yang mana. Verifikasi ini lebih dapat dipercaya daripada asumsi di
dokumen ini, terutama untuk `errgroup` dan `protobuf` yang dokumentasinya belum
sempat dibaca langsung.

### Single binary

Satu binary, dua mode lewat flag:

```bash
# LAPTOP 1 — master (menjalankan DUA server: HTTP untuk REST/UI, gRPC untuk cluster)
./distapi \
  --mode=master \
  --http-port=8080 \
  --grpc-port=9000 \
  --token=demo123

# LAPTOP 2 — node-1
./distapi \
  --mode=node \
  --node-id=node-1 \
  --grpc-port=9000 \
  --master=192.168.1.10:9000 \
  --advertise=192.168.1.11:9000 \
  --token=demo123

# LAPTOP 3 — node-2  : --node-id=node-2 --advertise=192.168.1.12:9000
# LAPTOP 4 — node-3  : --node-id=node-3 --advertise=192.168.1.13:9000
```

Ketiga node **boleh memakai port yang sama** (`9000`) karena berada di mesin
berbeda. IP di atas hanya contoh; gunakan IP asli tiap laptop.

- Vue di-build menjadi file statis lalu di-*embed* ke binary → satu file
  berisi API dan UI. Laptop yang **menjalankan** binary tidak perlu Node.js.
- Cross-compile satu kali per OS:

```bash
GOOS=linux   GOARCH=amd64 go build -o dist/distapi-linux   ./cmd/distapi
GOOS=windows GOARCH=amd64 go build -o dist/distapi.exe     ./cmd/distapi
GOOS=darwin  GOARCH=arm64 go build -o dist/distapi-mac     ./cmd/distapi
```

---

## 8. Library

### 8.1 Runtime (3 dependensi langsung)

| Library | Fungsi | Alasan masuk |
|---|---|---|
| `google.golang.org/grpc` | RPC master ↔ node | RPC wajib muncul; standar industri |
| `google.golang.org/protobuf` | Kontrak `.proto` | Dependensi wajib gRPC |
| `golang.org/x/sync` (`errgroup`) | Scatter–gather paralel dengan propagasi error dan cancel | Satu chunk gagal harus langsung ketahuan dan bisa dijadwalkan ulang; `WaitGroup.Go` (Go 1.25) tidak menangani error/context |

### 8.2 Dari stdlib (tanpa install)

`net/http` (routing method + wildcard, `PathValue`), `encoding/json`,
`log/slog`, `context`, `embed`, `image/*`, `sync`, `os/signal`, `flag`, `net`,
`testing`, `net/http/httptest`, `crypto/rand`.

### 8.3 Sengaja tidak dipakai

| Library | Alasan |
|---|---|
| gin / echo / fiber | `net/http` sejak Go 1.22 sudah cukup |
| viper / cobra | Config hanya belasan parameter |
| zap / logrus | `slog` sudah standar resmi |
| caarlos0/env | `flag` + `os.Getenv` cukup |
| go-playground/validator | Validasi upload cukup beberapa `if` |
| cenkalti/backoff | Retry kita pindah ke node lain, bukan mengulang ke server yang sama |
| google/uuid | `crypto/rand` cukup |
| testify | `testing` bawaan cukup |
| gorm / DB driver | Tidak ada database |
| wire / fx | DI lewat konstruktor cukup |

### 8.4 Opsional (keputusan tim)

- **`disintegration/imaging`** untuk resize berkualitas (Lanczos). Tanpanya,
  resize pakai stdlib (`image` + `image/draw`) dengan hasil lebih kasar. Repo
  stabil tetapi tidak aktif dikembangkan. Saran: stdlib dulu.

### 8.5 Tools pengembangan (bukan dependensi `go.mod`)

| Tool | Fungsi | Siapa yang butuh |
|---|---|---|
| `protoc` + `protoc-gen-go` + `protoc-gen-go-grpc` | Generate kode dari `.proto` | Hanya yang mengubah kontrak |
| `go vet`, `gofmt` | Analisis & format bawaan | Semua |
| `go test -race` | Deteksi race condition | Semua |

Hasil generate (`gen/`) **di-commit** ke repo, sehingga anggota yang tidak
menyentuh proto tidak perlu memasang `protoc`.

---

## 9. Konfigurasi

**Standar 12-Factor:** prioritas `flag > environment variable > default`.
Tanpa file config (YAML/JSON); tidak ada file yang harus dikopi ke 4 laptop.

| Parameter | Flag | Env | Default | Mode |
|---|---|---|---|---|
| Mode | `--mode` | `DISTAPI_MODE` | wajib | keduanya |
| Port HTTP (master) | `--http-port` | `DISTAPI_HTTP_PORT` | `8080` | master |
| Port gRPC | `--grpc-port` | `DISTAPI_GRPC_PORT` | `9000` | keduanya |
| Node ID | `--node-id` | `DISTAPI_NODE_ID` | hostname | node |
| Alamat master | `--master` | `DISTAPI_MASTER_ADDR` | wajib di node | node |
| Alamat diiklankan | `--advertise` | `DISTAPI_ADVERTISE_ADDR` | auto-detect | node |
| Interval heartbeat | `--heartbeat-interval` | env | `2s` | node |
| Batas node mati | `--node-timeout` | env | `6s` | master |
| Timeout task | `--task-timeout` | env | `30s` | master |
| Token cluster | `--token` | `DISTAPI_TOKEN` | wajib | keduanya |
| Log level | `--log-level` | env | `info` | keduanya |
| Jumlah worker | `--workers` | `DISTAPI_WORKERS` | node: `NumCPU`, master: `max(1, NumCPU/2)` | keduanya |
| Direktori data | `--data-dir` | `DISTAPI_DATA_DIR` | `./data` | master |
| Batas ukuran gambar | `--max-image-mb` | env | `5` | master |
| Maks gambar per job | `--max-images` | env | `20` | master |
| Retensi job | `--job-ttl` | env | `1h` | master |
| Maks percobaan ulang task | `--max-retries` | env | `3` | master |

- Master **tidak** dikonfigurasi daftar node; node mendaftar sendiri.
- Node harus mengiklankan **IP yang bisa dijangkau**, bukan `localhost` atau
  `0.0.0.0` (jebakan umum di WiFi).
- Validasi sekali saat startup; salah konfigurasi → berhenti dengan pesan jelas
  (*fail fast*).

---

## 9a. Penamaan (Naming)

> **Catatan verifikasi:** istilah *name*, *address*, *identifier*, dan *name
> resolution* di bagian ini berasal dari ringkasan evaluasi materi Pertemuan 5,
> **bukan dari pembacaan langsung slide 05**. Cocokkan dengan slide asli
> sebelum dipakai di laporan.

| Entity | Identifier (nama unik) | Address (lokasi) |
|---|---|---|
| Master | `master` | `192.168.1.10:9000` (gRPC), `192.168.1.10:8080` (HTTP) |
| Node 1 | `node-1` | `192.168.1.11:9000` |
| Node 2 | `node-2` | `192.168.1.12:9000` |
| Node 3 | `node-3` | `192.168.1.13:9000` |
| Job | `job_{id-acak}` | — (disimpan di master) |
| Task | `{job_id}-{index}` | — |

**Registry di master** memetakan `node_id → IP:port`. Itu adalah bentuk
sederhana *name resolution*: scheduler menyebut tujuan dengan nama (`node-2`),
dan registry menerjemahkannya menjadi alamat yang bisa dihubungi. Saat node
mati lalu hidup kembali dengan IP berbeda (umum di WiFi/DHCP), **identifier
tetap `node-2`, hanya address-nya yang diperbarui** melalui `Register` ulang.
Inilah alasan sistem memakai identifier, bukan menyimpan IP secara langsung.

---

## 10. Struktur Project

Konvensi `cmd/` + `internal/` (dirujuk panduan resmi go.dev *Organizing a Go
module*). Catatan: `golang-standards/project-layout` **bukan** standar resmi
tim Go; kita hanya mengambil bagian yang relevan (`cmd/`, `internal/`) dan
menolak `pkg/` serta struktur berlapis yang tidak dibutuhkan.

```
distapi/
├── cmd/distapi/main.go       # parse config, pilih mode, wiring, graceful shutdown
├── proto/cluster.proto       # kontrak gRPC
├── gen/                      # hasil generate protobuf (di-commit)
├── internal/
│   ├── config/               # flag + env, validasi
│   ├── api/                  # REST handler + serve frontend embed
│   ├── scheduler/            # pecah job, scatter-gather, retry/reschedule
│   ├── registry/             # daftar node, heartbeat, status (mutex)
│   ├── rpc/                  # gRPC server (node) & client (master)
│   ├── worker/               # logika resize + grayscale (dipakai master & node)
│   └── storage/              # file sementara (upload + hasil)
├── web/                      # Vue 3 + Vite
│   ├── src/
│   └── dist/                 # hasil build, di-embed ke binary
├── Makefile                  # build, test, proto, web, build-all
├── go.mod
└── README.md
```

**Pembagian kerja (minim konflik merge):**

| Anggota | Nama & NIM | Tanggung jawab | Folder utama |
|---|---|---|---|
| **A** | **Rafli Khiyanuran Bazhari** (240411100001) | Master core: scheduler, registry, fault tolerance | `scheduler/`, `registry/` |
| **B** | **Irma Annisatul Jannah** (240411100014) | gRPC: proto, server node, client master, heartbeat | `proto/`, `rpc/`, `gen/` |
| **C** | **Muhammad Fajar Nugroho** (240411100103) | REST API, storage, config, main | `api/`, `storage/`, `config/`, `cmd/` |
| **D** | **Zakaria Mujur Prasetyo** (240411100144) | Frontend Vue + worker (resize/grayscale) | `web/`, `worker/` |

Kontrak (`.proto` dan struct REST) disepakati **lebih dulu** agar keempatnya
bekerja paralel.

---

## 11. Praktik Industri yang Diterapkan

| Praktik | Implementasi |
|---|---|
| Structured logging | `log/slog`, `task_id` dan `job_id` di setiap log |
| Graceful shutdown | `signal.NotifyContext` + `http.Server.Shutdown` + `GracefulStop` gRPC |
| `context.Context` | Di semua panggilan antar-node (timeout dan pembatalan) |
| Timeout | `ReadHeaderTimeout` server; deadline di setiap RPC (default Go tidak punya timeout HTTP client) |
| Error wrapping | `fmt.Errorf("...: %w", err)` |
| Dependency injection | Lewat konstruktor dan interface kecil, tanpa global variable |
| Testing | Unit test registry/worker/scheduler; `httptest` untuk handler; `go test -race` |
| Keamanan dasar | Shared secret token: header `X-Cluster-Token` / metadata gRPC |
| Idempotency | Task ID deterministik `{job_id}-{index}`; hasil pertama menang; tulis atomik (temp file + `rename`) |
| Health check | `/healthz` + heartbeat |

**Delapan kesalahan klasik sistem terdistribusi** (*Fallacies of Distributed
Computing*) bisa menjadi kerangka narasi laporan: setiap praktik di atas adalah
jawaban atas satu asumsi yang salah (jaringan tidak reliabel, latensi tidak
nol, topologi berubah, dst.).

---

## 12. Rencana Demo

1. Nyalakan master + 3 node → UI menampilkan 4 node aktif.
2. Unggah banyak gambar → UI menunjukkan **gambar mana diproses laptop mana**.
3. **Demo fault tolerance:** cabut satu laptop dari WiFi di tengah proses →
   node ditandai mati dalam ≤ 6 detik → task dijadwalkan ulang → job tetap
   selesai dengan hasil benar.
4. Node dinyalakan kembali → mendaftar ulang otomatis.
5. **Benchmark:** waktu dengan 1, 2, 3, 4 node → grafik speedup untuk laporan.

**Urutan pengerjaan:**

1. Kontrak: `cluster.proto` + spesifikasi REST
2. Kerangka project: config, `main.go`, Makefile
3. Inti: registry, gRPC, worker, scheduler (+ test)
4. REST API + storage
5. Frontend Vue
6. Fault tolerance, demo script, benchmark, laporan

---

## 13. Keputusan Desain: Jawaban Default + Alasan

Bagian ini menjawab hasil *grilling* terhadap draf pertama. Setiap poin berisi
**keputusan default**, **alasan**, dan **apa yang bisa mengubah keputusan itu**.

**Cara membaca status:**

| Status | Arti |
|---|---|
| ✅ **Diputuskan** | Keputusan desain murni; bisa diambil tanpa informasi tambahan |
| 🟡 **Default + perlu verifikasi** | Keputusan masuk akal, tetapi bergantung pada asumsi yang harus dites |
| 🔴 **Tidak bisa diputuskan dari sini** | Butuh informasi dari dosen atau tes nyata di laptop tim |

> Ini adalah **pendapat dan rekomendasi**, bukan fakta. Tim boleh menolak
> selama alasannya jelas dan dicatat.

---

### 13.1 Risiko yang bisa menggagalkan proyek

#### D1. Konektivitas jaringan antar-laptop 🔴

**Masalah:** WiFi kampus/publik sering menerapkan *client isolation* (laptop di
jaringan yang sama tidak bisa saling terhubung). Firewall Windows juga
memblokir koneksi masuk secara default.

**Keputusan:** Ini **tugas nol**, dikerjakan sebelum menulis kode. Kerjakan
urutan berikut di keempat laptop:

1. Laptop dihubungkan ke jaringan yang sama.
2. Catat IP masing-masing.
3. Jalankan server TCP sederhana di satu laptop dan coba konek dari tiga
   lainnya, semua arah.
4. Jika gagal: pakai **hotspot HP** salah satu anggota sebagai jaringan
   cadangan, atau router kecil.

**Alasan:** Ini satu-satunya risiko yang tidak bisa diperbaiki dengan kode.
Semua desain lain runtuh jika laptop tidak bisa saling bicara, dan kegagalan
ini paling mahal jika baru ketahuan saat demo.

**Yang bisa mengubah keputusan:** Hasil tes. Jika hanya satu arah yang jalan
(node bisa konek ke master, tetapi master tidak bisa konek ke node), lihat D4.

**Catatan Windows:** izinkan port melalui firewall (satu kali per laptop).
Untuk demo di jaringan tertutup, membuat aturan untuk aplikasi `distapi.exe`
lebih aman daripada mematikan firewall.

---

#### D2. Rubrik penilaian dosen 🔴

**Masalah:** Seluruh desain mengasumsikan kriteria adalah "aplikasi web
RESTful + RPC + memakai 4 laptop". Rubrik tertulis belum pernah dilihat.

**Keputusan:** **Tidak bisa saya putuskan.** Sebelum menulis kode, tim harus
mendapatkan tiga hal dari dosen atau dokumen tugas:

1. Kriteria penilaian tertulis (jika ada).
2. Konsep yang wajib muncul (mis. replikasi, konsistensi, sinkronisasi waktu,
   penamaan, konsensus, fault tolerance).
3. Format deliverable (laporan, video demo, presentasi, kode di repo).

**Alasan:** Desain ini kuat di: arsitektur, komunikasi (RPC), fault tolerance,
scalability, transparansi. Desain ini **lemah di**: replikasi data,
konsistensi, konsensus, dan sinkronisasi waktu, karena worker sengaja
*stateless*. Jika rubrik menuntut konsep-konsep itu, desain perlu diubah
**sekarang**, bukan setelah tiga minggu coding.

**Rencana cadangan jika rubrik menuntut replikasi/konsistensi:** tambahkan
penyimpanan hasil yang **direplikasi ke 2 node** (setiap hasil disimpan di dua
laptop). Ini murah, menambah konsep replikasi, dan tidak merusak arsitektur
yang ada. Jangan ditambahkan sebelum ada kebutuhan yang terkonfirmasi.

---

#### D3. gRPC sebagai "RPC" 🔴

**Keputusan:** Tetap **gRPC**. Tanyakan konfirmasi ke dosen.

**Alasan:** gRPC adalah implementasi RPC modern yang sah secara teknis
(pemanggilan prosedur jarak jauh dengan kontrak `.proto`). Risikonya bukan
teknis, melainkan definisi: sebagian dosen memakai "RPC" untuk merujuk materi
kuliah (RMI, XML-RPC, socket).

**Cadangan jika gRPC ditolak:** JSON-RPC di atas HTTP. Perubahan hanya di
lapisan komunikasi (`internal/rpc`); scheduler dan worker tidak berubah. Inilah
alasan *interface* `NodeClient` dipertahankan, karena mengganti protokol
menjadi murah.

---

### 13.2 Lubang desain

#### D4. Arah heartbeat dan siapa yang menghubungi siapa ✅ (dengan syarat D1)

**Keputusan:** **Node → master untuk registrasi dan heartbeat. Master → node
untuk mengirim task.** (Model *push* untuk task.)

**Alasan:**

- Registrasi dan heartbeat dimulai oleh node, sehingga master tidak perlu tahu
  IP node sebelumnya (service discovery otomatis).
- Push lebih sederhana dijelaskan dan diimplementasikan daripada pull: alur
  "master memberi tugas, node mengerjakan" langsung sesuai skema master-worker.
- Untuk 4 laptop di satu jaringan, kelemahan push tidak signifikan.

**Konsekuensi:** master **harus bisa menjangkau** node. Jika tes D1 menunjukkan
master tidak bisa konek ke node tetapi sebaliknya bisa, beralihlah ke model
**pull** (node mengambil task lewat RPC ke master; lebih toleran firewall).
Perubahan ini kecil di kontrak proto tetapi nyata di scheduler, jadi
diputuskan **sebelum** kode scheduler ditulis.

**Pertimbangan pull (jika dibutuhkan):** pull juga menyelesaikan D12
(spesifikasi laptop berbeda), karena node cepat otomatis mengambil lebih banyak
task. Ini kelebihan nyata, tetapi menambah kerumitan (antrian task di master,
long-polling atau streaming). Belum dipakai sebagai default karena kerumitannya
belum terbukti sepadan.

---

#### D5. Pembagian task: satu gambar = satu task ✅

**Keputusan:** **Satu gambar = satu task.** Demo **selalu memakai banyak gambar
(disarankan 12–20)**.

**Alasan:**

- Memecah satu gambar menjadi potongan (tile) lalu menggabungkannya kembali
  jauh lebih rumit (resize butuh konteks piksel di tepi potongan) dan bukan
  inti mata kuliah.
- Dengan 12–20 gambar, keempat laptop pasti terpakai dan grafik speedup
  bermakna.
- Task per gambar secara alami **idempotent** (input sama → output sama).

**Konsekuensi yang harus diterima:** jika pengguna mengunggah 1–3 gambar, tidak
semua laptop terpakai. Tambahkan **pesan di UI**: "Unggah lebih dari 4 gambar
untuk memanfaatkan seluruh node". Ini keterbatasan yang jujur, bukan bug.

**Yang bisa mengubah keputusan:** jika dosen menuntut satu pekerjaan besar yang
dibagi ke semua node, pertimbangkan varian yang membagi *rentang komputasi*
(bukan gambar), misalnya menghitung batch thumbnail dari satu arsip ZIP.

---

#### D6. Cara mengirim gambar ke node ✅

**Keputusan:** **gRPC unary**, dengan batas **5 MB per gambar** dan maksimum
**20 gambar per job**. Batas pesan gRPC dinaikkan eksplisit di server dan klien
(`MaxRecvMsgSize` / `MaxSendMsgSize`, mis. 8 MB).

**Alasan:**

- Batas default penerimaan pesan gRPC adalah 4 MB. Foto dari HP sering lebih
  besar, sehingga tanpa batas dan pesan error yang jelas, unggahan gagal dengan
  cara yang membingungkan.
- Streaming (client streaming per potongan byte) lebih benar untuk file besar,
  tetapi menambah kode dan kasus kegagalan (pemutusan di tengah stream). Untuk
  gambar ≤ 5 MB, unary cukup dan lebih mudah dites.
- Validasi dilakukan **di API layer**, sehingga file yang terlalu besar ditolak
  cepat dengan `413 Payload Too Large`, sebelum menyentuh jaringan antar-node.

**Yang bisa mengubah keputusan:** kebutuhan memproses gambar > 5 MB. Saat itu
beralihlah ke *client streaming* dengan potongan 64–256 KB.

**Format yang diterima:** JPEG dan PNG saja (yang didukung `image/jpeg` dan
`image/png` di stdlib). Tolak format lain dengan `415 Unsupported Media Type`.

---

#### D7. Hasil ganda saat node lambat dianggap mati ✅

**Keputusan:** **Hasil pertama yang sampai dipakai; sisanya dibuang.** Penulisan
hasil bersifat **atomik** (tulis ke file sementara, lalu `rename`).

**Alasan:**

- Kegagalan RPC atau timeout tidak membuktikan worker mati; worker bisa hanya
  lambat. Karena itu dua node bisa mengerjakan gambar yang sama.
- Karena task per gambar **idempotent** (D5), dua hasil yang sampai isinya
  sama. Yang penting hanya: satu task tidak dihitung dua kali dan file hasil
  tidak rusak.
- `rename` atomik menjamin tidak ada file setengah jadi yang terlihat pengguna.

**Aturan yang dijamin scheduler:**

1. Setiap task punya ID deterministik: `{job_id}-{index}`.
2. Status task hanya bergerak maju: `PENDING → RUNNING → DONE / FAILED`.
3. Saat hasil datang untuk task yang sudah `DONE`, hasil itu **diabaikan**
   (dicatat di log, tidak menimpa).
4. Task dijadwalkan ulang maksimal **3 kali**. Setelah itu ditandai `FAILED`
   dan job menampilkan kegagalan sebagian, bukan menggantung selamanya.

**Jaminan yang diklaim di laporan** (rumusan yang aman secara akademik):

> *"Sistem menggunakan **at-least-once execution**. Dampak duplikasi
> diminimalkan melalui **idempotent processing** dan mekanisme
> **first-result-wins**."*

Jangan menulis "exactly-once", baik untuk eksekusi maupun untuk efek: istilah
itu mengundang perdebatan definisi dan tidak bisa dijamin penuh di sistem ini.

---

#### D8. Master kewalahan karena ikut bekerja ✅

**Keputusan:** **Master ikut bekerja, tetapi dengan batas konkurensi lebih
rendah daripada node.** Worker lokal di master dibatasi `max(1, NumCPU/2)`
goroutine pemroses; node memakai `NumCPU`.

**Alasan:**

- Persyaratan "semua laptop terpakai" mengharuskan master ikut memproses.
- Master juga melayani REST, menyajikan UI, menerima heartbeat dari node, dan
  memantau kesehatan node. Jika CPU master penuh oleh resize, tiga hal ikut
  terganggu: respons API dan UI melambat, **heartbeat dari node bisa terlambat
  diproses sehingga node sehat salah ditandai mati**, dan pengiriman task
  tertunda. Risiko kedua paling berbahaya karena memicu penjadwalan ulang yang
  tidak perlu.
- Master tidak mengirim heartbeat ke dirinya sendiri; worker lokal selalu
  dianggap aktif selama proses master hidup.
- Batas konkurensi murah dan menjaga master tetap responsif.

**Pengaturan lain:** jumlah worker per node dibuat bisa dikonfigurasi
(`--workers`), agar tim bisa menyetel saat benchmark.

**Yang bisa mengubah keputusan:** hasil benchmark. Jika UI tetap tersendat,
turunkan lagi jatah master; jika master selalu menganggur, naikkan.

---

### 13.3 Perilaku pada kondisi khusus

#### D9. Master restart di tengah job ✅

**Keputusan:** **Dicatat sebagai keterbatasan, tidak dipulihkan.** State job
berada di memori; setelah restart, semua job hilang dan node yang masih hidup
mendaftar ulang otomatis.

**Alasan:** memulihkan job memerlukan penyimpanan persisten (database atau
file log) dan logika *recovery*, yaitu menambah tier dan kerumitan yang
dinyatakan di luar scope. Kelemahan ini justru materi laporan yang baik
(master sebagai *Single Point of Failure*).

**Yang bisa mengubah keputusan:** jika rubrik menuntut *high availability*.
Opsi ringan berikutnya: tulis status job ke satu file JSON secara berkala
(*snapshot*), bukan database.

---

#### D10. Semua node mati kecuali master ✅

**Keputusan:** **Degradasi halus.** Master mengerjakan semua task sendiri dan UI
menampilkan peringatan "hanya 1 dari 4 node aktif".

**Alasan:** sistem yang tetap memberi jawaban benar (meski lebih lambat) lebih
meyakinkan saat demo fault tolerance daripada menolak pekerjaan. Ini juga
tepat sebagai definisi *graceful degradation*.

---

#### D11. Penyimpanan hasil dan pembersihan ✅

**Keputusan:**

- Hasil disimpan di direktori sementara pada **master saja**, di
  `{data-dir}/jobs/{job_id}/`.
- Job dihapus otomatis setelah **1 jam** oleh proses pembersih berkala, atau
  manual lewat `DELETE /api/v1/jobs/{id}`.
- Saat startup, master **mengosongkan** `{data-dir}/jobs/`.

**Alasan:** node stateless (tidak menyimpan hasil), sehingga menghapus atau
menambah node tidak memerlukan migrasi data. Pembersihan otomatis mencegah disk
penuh setelah demo berulang, dan pengosongan saat startup konsisten dengan
keputusan D9 (job tidak dipulihkan).

**Konsekuensi:** master menyimpan semua hasil, jadi master menanggung beban
disk dan I/O. Untuk 20 gambar × 5 MB ini kecil. Batasi total per job
(mis. 100 MB) untuk keamanan.

---

#### D12. Laptop dengan spesifikasi berbeda 🟡

**Keputusan:** **Distribusi push dengan dispatch dinamis, bukan pembagian rata
di awal.** Master menyimpan antrian task; setiap node yang **sedang kosong**
(jumlah task berjalan < kapasitasnya) menerima task berikutnya.

**Alasan:**

- Pembagian rata di awal (`10 gambar / 4 node`) membuat laptop cepat menunggu
  laptop lambat, sehingga grafik speedup jelek dan sulit dijelaskan.
- Dispatch dinamis (istilah: *work stealing* sederhana / *dynamic load
  balancing*) memberi lebih banyak task ke node cepat secara otomatis, tetap
  dengan model push.
- Kerumitannya sedang: scheduler menyimpan hitungan task berjalan per node,
  bukan struktur baru.

**Status 🟡:** ini rekomendasi, tetapi versi paling sederhana (round-robin) juga
sah untuk tugas. Jika waktu sempit, mulai dari round-robin dan tingkatkan
kemudian, karena antarmuka `NodeClient` dan `scheduler` sudah memisahkan
logikanya.

---

### 13.4 Proses tim

#### D14. Integrator ✅

**Keputusan:** Tunjuk **satu integrator** (disarankan anggota C, yang memegang
`main.go`, konfigurasi, dan REST). Tanggung jawab: menjaga `main` selalu bisa
dibuild, memimpin penggabungan mingguan, dan membangun binary rilis untuk demo.

**Alasan:** empat orang mengerjakan empat folder, dan kegagalan paling umum
adalah integrasi pertama yang terjadi di minggu terakhir. Integrasi kecil dan
sering jauh lebih murah.

**Aturan sederhana yang disarankan:**

1. Satu cabang `main` yang selalu hijau (`go build ./...` dan `go test ./...`).
2. Cabang fitur per orang, digabung lewat pull request dengan minimal satu
   peninjau.
3. **Integrasi awal di minggu pertama** dengan versi "kerangka" (master dan
   node bisa saling mendaftar dan bertukar satu RPC dummy), sebelum ada fitur
   nyata.

---

#### D15. Prioritas jika waktu tidak cukup ✅

**Urutan yang tidak boleh dikorbankan (dari paling penting):**

1. **Master + 3 node berjalan di 4 laptop dan saling bertukar RPC.**
2. **Scatter-gather nyata:** gambar diproses di semua node, hasil digabung.
3. **Fault tolerance:** node mati → task dijadwalkan ulang → job selesai.
4. **UI Vue** yang menampilkan progres dan node.
5. Benchmark speedup.
6. CI dan kerapian tambahan.

**Alasan:** poin 1–3 adalah inti mata kuliah dan inti demo. UI (4) memperkuat
demo tetapi bisa dikorbankan menjadi halaman sangat sederhana; tanpa poin 1–3,
UI cantik tidak berarti.

**Aturan pemotongan:** jika tertinggal, potong dari bawah (6 → 5 → 4 dalam
bentuk yang lebih sederhana), tidak pernah dari atas.

---

#### D16. Kemampuan tim dengan Go dan gRPC 🔴

**Masalah:** desain mengasumsikan keempat anggota bisa menulis Go. Tidak
diketahui.

**Keputusan default:** **jika ada anggota yang belum pernah memakai Go,**
tugaskan mereka ke bagian dengan risiko terendah dan belajar tercepat:

| Kemampuan | Penempatan yang disarankan |
|---|---|
| Belum pernah Go, mahir JavaScript | Frontend Vue (D) |
| Belum pernah Go, mahir bahasa lain | `worker/` (fungsi murni: input gambar → output gambar) dan test |
| Paling mahir Go | Scheduler + registry (A) dan integrator |
| Paling nyaman dengan jaringan/protokol | gRPC (B) |

**Alasan:** scheduler dan registry paling sarat konkurensi (mutex, goroutine,
context) dan paling rawan *race condition*; paling aman dipegang yang paling
berpengalaman.

---

### 13.5 Asumsi angka yang harus diuji, bukan dipercaya

| Parameter | Nilai awal | Sumber | Risiko |
|---|---|---|---|
| Interval heartbeat | 2 detik | Kebiasaan umum | Aman |
| Batas node mati | 6 detik (3× interval) | Praktik umum "3 heartbeat terlewat" | Di WiFi tidak stabil, bisa salah menandai node mati |
| Timeout task | 30 detik | Tebakan | Bergantung ukuran gambar dan kecepatan laptop |
| Batas percobaan ulang | 3 | Tebakan | Aman untuk tugas |
| Batas ukuran gambar | 5 MB | Keputusan D6 | Tidak masalah |

**Angka-angka ini adalah titik awal yang masuk akal, bukan hasil pengukuran.**
Ukur di jaringan asli sebelum demo, dan jadikan semuanya konfigurasi (Bagian 9)
sehingga bisa disetel tanpa mengubah kode.

---

### 13.6 Ringkasan status

| # | Topik | Status | Bergantung pada |
|---|---|---|---|
| D1 | Konektivitas jaringan | 🔴 | Tes nyata |
| D2 | Rubrik dosen | 🔴 | Dosen |
| D3 | gRPC sebagai RPC | 🔴 | Dosen |
| D4 | Arah heartbeat/task | ✅ | D1 |
| D5 | Satu gambar = satu task | ✅ | D2 |
| D6 | Unary + batas 5 MB | ✅ | — |
| D7 | Hasil pertama menang, atomik | ✅ | — |
| D8 | Batas konkurensi master | ✅ | Benchmark |
| D9 | Master restart | ✅ | D2 |
| D10 | Semua node mati | ✅ | — |
| D11 | Penyimpanan + pembersihan | ✅ | — |
| D12 | Dispatch dinamis | 🟡 | Waktu |
| D14 | Integrator | ✅ | — |
| D15 | Prioritas | ✅ | — |
| D16 | Kemampuan tim | 🔴 | Tim |

**Poin 🔴 yang memblokir penulisan kode:** D1, D2, dan D3. Sisanya sudah
punya keputusan default yang bisa langsung dipakai.

> D13 (versi Go di keempat laptop) sengaja dihapus dari daftar; kebijakan
> versi Go kini ada di Bagian 7. D16 (kemampuan tim) tetap 🔴 tetapi tidak
> memblokir kode, hanya memengaruhi pembagian tugas.

---

## 14. Catatan Verifikasi

Bagian ini memisahkan **apa yang benar-benar sudah dibaca dari sumbernya**
dari **apa yang masih asumsi**, supaya laporan tidak mengutip asumsi sebagai
fakta.

### 14.1 Terverifikasi langsung dari sumber primer

| Klaim | Sumber |
|---|---|
| Routing method + wildcard di `net/http` sejak Go 1.22 | go.dev (catatan rilis Go 1.22) |
| Go 1.25 (Agustus 2025) menambah `sync.WaitGroup.Go`, `testing/synctest`, GOMAXPROCS sadar-container | go.dev/doc/go1.25 (dibaca langsung) |
| Go 1.26 dirilis 10 Februari 2026; Go 1.27 dirilis 19 Agustus 2026 | go.dev/doc/devel/release (dibaca langsung) |
| Kebijakan: setiap rilis mayor didukung sampai ada dua rilis mayor yang lebih baru | go.dev/doc/devel/release |
| Patch terbaru per 21 Sep 2026: Go 1.27.1 dan 1.26.8 (keduanya 1 Sep 2026); 1.25.14 (19 Agu 2026) | go.dev/doc/devel/release |
| gRPC-Go mensyaratkan Go minimum 1.25 sejak v1.81.0 | github.com/grpc/grpc-go/releases (dibaca langsung) |
| gRPC-Go mendukung "dua rilis mayor Go terbaru" | README grpc-go dan pkg.go.dev |
| gRPC-Go rilis terbaru v1.83.2 (25 Agu 2026) | github.com/grpc/grpc-go/releases |
| Validasi path ketat sejak v1.79.3 tidak bisa lagi dimatikan sejak v1.82.0 | catatan rilis v1.82.0 |
| Konvensi `cmd/` + `internal/` | go.dev, *Organizing a Go module* |
| Definisi Master-Slave, Peer-to-Peer, tiering, layering, tiga layer (Platform, Middleware, Applications) | **Slide 04_Arsitektur.pdf (dibaca langsung)** |
| Tugas: mendemokan aplikasi web dengan arsitektur RESTful API; bahasa bebas (termasuk Go dan JS) | **Slide 06_Restful_API-Demo.pdf (dibaca langsung)** |

### 14.2 Berasal dari artikel komunitas atau ringkasan pihak ketiga (bukan primer)

| Klaim | Catatan |
|---|---|
| "REST di tepi, gRPC di dalam" sebagai praktik industri | Artikel blog; untuk laporan akademis, kutip sumber yang lebih kuat |
| Angka heartbeat "3 interval" | Kebiasaan umum, bukan standar |
| Semua klaim tentang isi slide Pertemuan 1, 2, 3, dan 5 | Berasal dari **evaluasi ChatGPT** milik anggota tim; slide aslinya **belum dibaca** |
| Istilah *name / address / identifier / name resolution* | Sama seperti di atas; cocokkan dengan slide 05 |
| Rujukan RPC call semantics dan Amdahl's Law | Sama seperti di atas; cocokkan dengan slide 02 dan 03 |

Untuk klaim fundamental sistem terdistribusi, laporan sebaiknya mengutip paper
MapReduce (Dean & Ghemawat, 2004) dan slide kuliah, bukan blog.

### 14.3 Belum diverifikasi sama sekali

- **Dokumentasi `golang.org/x/sync` (errgroup) dan `google.golang.org/protobuf`:**
  situs `pkg.go.dev` tidak bisa diakses saat riset. Kesesuaian keduanya dengan
  lantai `go 1.25.0` adalah **inferensi**; buktikan dengan `go mod tidy`
  (lihat Bagian 7).
- **Dukungan eksplisit gRPC-Go terhadap Go 1.27:** README menyatakan "dua rilis
  terbaru" tanpa menyebut nomor versi. Secara logika mencakup 1.27, tetapi tidak
  ada pernyataan eksplisit yang ditemukan.
- **Rubrik, deadline, dan format deliverable dosen:** belum diketahui. Slide 04
  memuat halaman "P1 is out. Design report is due on September 22 / PS2 ... due
  on October 1" berbahasa Inggris; **tidak diketahui apakah itu berlaku untuk
  kelompok ini** (bisa saja sisa template kuliah). Tanyakan ke dosen.
- **Kode:** belum ada yang ditulis, dan belum ada yang dikompilasi atau diuji.
  Container pengembangan yang dipakai saat menyusun dokumen ini memiliki Go
  1.22.2, sehingga tidak bisa membangun gRPC-Go terbaru (butuh 1.25+).