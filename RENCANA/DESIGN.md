# Distributed Image Processing Service

**Tugas kelompok · Sistem Terdistribusi (IF2228) · Universitas Trunojoyo Madura**
Tim 4 orang · 4 laptop (1 master + 3 node) · Go single binary · Vue.js

> **Cara membaca dokumen ini.** Setiap keputusan penting diberi rujukan ke slide
> kuliah dengan format **[S04 h.14]** = *Slide 04 Arsitektur, halaman 14*.
> Rujukan sudah dicocokkan dengan PDF asli. Tanda 🟡 berarti *keputusan kami,
> bukan dari materi*. Tanda ⚠️ berarti *belum terverifikasi*.

| Kode | Slide |
|---|---|
| **S02** | 02 Networking |
| **S03** | 03 RPC |
| **S04** | 04 Arsitektur |
| **S05** | 05 Penamaan |
| **S06** | 06 RESTful API (Demo) |

---

## Daftar Isi

1. [Ringkasan dan Ruang Lingkup](#1-ringkasan-dan-ruang-lingkup)
2. [Arsitektur](#2-arsitektur)
3. [Layering](#3-layering)
4. [Tiering](#4-tiering)
5. [Komunikasi: REST dan gRPC](#5-komunikasi-rest-dan-grpc)
6. [Penamaan](#6-penamaan)
7. [Fault Tolerance dan Semantik RPC](#7-fault-tolerance-dan-semantik-rpc)
8. [Tech Stack dan Library](#8-tech-stack-dan-library)
9. [Konfigurasi dan Menjalankan](#9-konfigurasi-dan-menjalankan)
10. [Struktur Project dan Pembagian Kerja](#10-struktur-project-dan-pembagian-kerja)
11. [Praktik Engineering](#11-praktik-engineering)
12. [Rencana Demo dan Benchmark](#12-rencana-demo-dan-benchmark)
13. [Keputusan Terbuka dan Risiko](#13-keputusan-terbuka-dan-risiko)
14. [Peta Materi ke Proyek](#14-peta-materi-ke-proyek)

---

## 1. Ringkasan dan Ruang Lingkup

**Apa yang dibuat:** aplikasi web tempat pengguna mengunggah banyak gambar.
Sistem membagi gambar ke **4 laptop** yang memprosesnya paralel (resize +
grayscale), lalu hasilnya ditampilkan dan bisa diunduh.

**Syarat tugas:** setiap kelompok mendemokan aplikasi web dengan arsitektur
RESTful API; bahasa bebas termasuk Go dan JS **[S06 h.3]**.

**Aturan main proyek:**
- Praktik industri **yang murah dan bernilai**, tanpa over-engineering.
- Stdlib dulu; library eksternal hanya jika benar-benar mengurangi risiko bug.
- Setiap keputusan harus bisa dijelaskan alasannya saat demo.

| Termasuk | Sengaja dilewati (dicatat sebagai *future work*) |
|---|---|
| Upload banyak gambar lewat web | Database, autentikasi pengguna |
| Distribusi task ke 4 laptop (master ikut bekerja) | TLS/mTLS, Docker, Kubernetes |
| Progres job dan status node langsung di UI | Metrics/tracing, circuit breaker |
| Deteksi node mati dan penjadwalan ulang otomatis | Leader election, message queue |
| Unduh hasil | Vue Router, Pinia |

> **Scope dibekukan.** Tambahan fitur apa pun ditolak sampai tugas selesai.

---

## 2. Arsitektur

### 2.1 Pilihan: Master-Slave

Slide menyebut dua arsitektur utama: **Master-Slave** (peran entitas
*asimetris*) dan **Peer-to-Peer** (peran *simetris*) **[S04 h.12]**. Proyek ini
memakai **Master-Slave**. Dokumen ini menulis "master-worker"; itu istilah yang
sama (gambar slide juga memakai label *Worker*).

```
                    ┌─────────────────────┐
                    │  Browser (Vue.js)   │
                    └──────────┬──────────┘
                               │ REST / JSON
                               ▼
                 ┌──────────────────────────┐
                 │  LAPTOP 1 — MASTER       │
                 │  REST API + UI statis    │
                 │  Scheduler               │
                 │  Registry + heartbeat    │
                 │  Worker lokal            │
                 └───┬──────────┬───────────┘
              gRPC   │          │          │  gRPC
            ┌────────┘          │          └────────┐
            ▼                   ▼                   ▼
     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
     │ LAPTOP 2    │     │ LAPTOP 3    │     │ LAPTOP 4    │
     │ node-1      │     │ node-2      │     │ node-3      │
     │ Worker      │     │ Worker      │     │ Worker      │
     └─────────────┘     └─────────────┘     └─────────────┘
```

### 2.2 Peran master sesuai slide

Slide mendefinisikan master sebagai *Central Coordinator/Manager* **[S04 h.14]**.
Pemetaannya ke proyek:

| Peran master menurut slide | Di proyek ini |
|---|---|
| Menangani request klien | REST API `/api/v1/jobs` |
| Memantau resource sistem | Registry + heartbeat node |
| Menjadwalkan job ke slave | Scheduler (1 gambar = 1 task) |
| Melacak progres di slave | Status task `PENDING → RUNNING → DONE / FAILED` |
| Mengagregasi hasil | Scatter–gather di master |
| Melapor ke klien | UI polling `GET /api/v1/jobs/{id}` |

Peran slave: *melakukan task yang diberikan master dan melapor balik* **[S04 h.14]**
→ `Worker.ProcessImage`.

### 2.3 Kelemahan yang kami akui

Slide menyebut tiga karakteristik Master-Slave **[S04 h.15]**. Ketiganya berlaku
di proyek ini, dan kami menuliskannya, bukan menyembunyikannya:

| Karakteristik (slide) | Berlaku di proyek ini |
|---|---|
| Node tidak setara → rentan **Single-Point-of-Failure** | Master mati → sistem berhenti, job hilang |
| Master sebagai **central coordinator** → keputusan mudah | Scheduling terpusat, tanpa konsensus |
| Tidak bisa **scale out** tanpa batas → master jadi **bottleneck** | Master menerima upload, menyimpan hasil, dan mengirim semua task |

**Kenapa bukan Peer-to-Peer?** Slide menyebut P2P *tidak punya SPOF* dan *bisa
scale out tanpa batas*, tetapi tanpa koordinator pusat *pengambilan keputusan
menjadi lebih sulit* **[S04 h.18]**. Kami sadar P2P unggul di dua hal pertama,
namun memilih Master-Slave karena tugas ini menuntut pembagian pekerjaan yang
mudah didemokan, dan koordinasi terpusat jauh lebih sederhana. Ini trade-off
yang disengaja, bukan ketidaktahuan. 🟡

### 2.4 Bukan microservice 🟡

| Aspek | Microservice | Proyek ini |
|---|---|---|
| Dipecah berdasarkan | Fungsi bisnis | Beban kerja |
| Kode tiap instance | Berbeda | **Sama** (single binary) |
| Tujuan | Modularitas | Paralelisme |

Jawaban singkat jika ditanya dosen: *"Ini Master-Slave dengan scatter-gather.
Kode di semua laptop sama; yang berbeda hanya perannya, ditentukan lewat flag."*

---

## 3. Layering

Slide: *layering = organisasi vertikal layanan; lapisan atas memakai layanan
lapisan bawah dan kompleksitas lapisan bawah disembunyikan* **[S04 h.34]**.

### 3.1 Tiga layer besar (sesuai slide)

Sistem terdistribusi diorganisasi menjadi **Platform → Middleware → Applications**
**[S04 h.35]**.

| Layer | Di proyek ini |
|---|---|
| **Applications** | Scheduler, worker, REST API, aplikasi Vue |
| **Middleware** | **gRPC + Protocol Buffers** |
| **Platform** | OS dan hardware laptop, jaringan WiFi/LAN |

Slide RPC menempatkan *Remote Invocation* tepat di lapisan middleware, di atas
IPC primitives (socket) dan transport TCP/UDP **[S03 h.54]**. Middleware
"menyembunyikan heterogenitas dan menyederhanakan pemrograman dengan
mengabstraksi mekanisme komunikasi" **[S04 h.35]**; artinya scheduler cukup
memanggil `ProcessImage(...)` tanpa mengurus socket dan format byte.

### 3.2 Layering di dalam kode (turunan layer *Applications*) 🟡

```
┌───────────────────────────────────────────────┐
│ 1. API            internal/api                │  REST handler, validasi input
├───────────────────────────────────────────────┤
│ 2. Coordination   internal/scheduler          │  pecah job, jadwalkan, retry
├───────────────────────────────────────────────┤
│ 3. Communication  internal/rpc                │  klien & server gRPC, timeout
├───────────────────────────────────────────────┤
│ 4. State          internal/registry, storage  │  daftar node, file sementara
└───────────────────────────────────────────────┘
      internal/worker  → logika resize + grayscale (dipakai master & node)
```

Aturan: lapisan atas hanya memanggil lapisan di bawahnya; kontrak antar-lapisan
berupa tipe eksplisit; detail transport (status HTTP, error gRPC) tidak bocor ke
lapisan koordinasi.

Satu praktik *hexagonal* yang murah: scheduler bergantung pada interface kecil
`NodeClient`, sehingga bisa dites dengan *fake* tanpa jaringan dan protokolnya
bisa diganti tanpa menyentuh scheduler.

---

## 4. Tiering

Slide: *tiering adalah teknik untuk (1) mengorganisasi fungsi sebuah layanan dan
(2) menempatkan fungsi itu ke server yang sesuai; tiering = pemisahan
**horizontal**, layering = organisasi **vertikal**; keduanya saling melengkapi*
**[S04 h.27–28]**.

### 4.1 Pemetaan ke contoh tiga tier di slide

Slide memberi contoh tiga tier: *Presentation*, *Application*, *Data Logic*
**[S04 h.30–31]**.

| Tier (slide) | Di proyek ini | Mesin |
|---|---|---|
| **Tier 1 — Presentation Logic** | Aplikasi Vue di browser | Laptop mana saja |
| **Tier 2 — Application Logic** | REST API, scheduler, registry | Laptop 1 (master) |
| **Tier 3 — Data Logic** | **Diganti Processing tier**: worker pemroses gambar | Laptop 1–4 |

> **Jujur soal Tier 3.** Contoh slide berisi *database manager*; proyek ini
> tidak punya database. Jadi ini **variasi tiga tier**, bukan tiga tier klasik.
> Sebut demikian saat presentasi.

### 4.2 Kelebihan dan kekurangan (dari slide, berlaku juga di sini)

Slide **[S04 h.32]** menyebut kelebihan: pemetaan satu-satu dari elemen logis ke
server fisik (mudah dirawat) dan peran tiap tier jelas. Kekurangan: kompleksitas
mengelola banyak server, tambahan trafik jaringan, dan tambahan latensi.

Kekurangan itu bukan teori di sini: **benchmark kami akan mengukurnya** (Bagian 12).

### 4.3 Catatan
- Master menjalankan **dua peran** (Tier 2 + worker Tier 3) di satu mesin.
- Tidak ada data tier karena worker *stateless*. Menambahkannya tanpa kebutuhan
  nyata adalah over-engineering. 🟡

---

## 5. Komunikasi: REST dan gRPC

**Pola: REST di tepi, gRPC di dalam.** 🟡

| Jalur | Protokol | Alasan |
|---|---|---|
| Browser → Master | **REST/JSON** | Tugas mensyaratkan RESTful **[S06 h.3]**; browser tidak bisa gRPC native |
| Master ↔ Node | **gRPC (protobuf)** | Remote invocation **[S03]**, efisien untuk data biner (gambar) |

REST memenuhi tugas Pertemuan 6; gRPC menunjukkan penerapan materi RPC
Pertemuan 3. Keduanya muncul, masing-masing punya alasan.

### 5.1 REST API (publik)

| Method | Endpoint | Fungsi | Status |
|---|---|---|---|
| POST | `/api/v1/jobs` | Buat job (upload gambar) | 202 Accepted |
| GET | `/api/v1/jobs` | Daftar job | 200 |
| GET | `/api/v1/jobs/{id}` | Detail dan progres | 200 / 404 |
| GET | `/api/v1/jobs/{id}/results/{file}` | Unduh hasil | 200 / 404 |
| DELETE | `/api/v1/jobs/{id}` | Batalkan / hapus | 204 / 404 |
| GET | `/api/v1/nodes` | Status node | 200 |
| GET | `/healthz` | Health check | 200 |

Ciri RESTful yang dipenuhi: resource berupa kata benda, method sesuai makna,
status code benar, stateless, format error seragam, versi `/api/v1`.
**Asynchronous request–reply:** `POST` langsung membalas `202` + ID job; UI
memantau lewat `GET` sampai selesai.

### 5.2 gRPC (internal): dua service

Arah panggilan berbeda, jadi kontrak dipecah dua:

```proto
// Berjalan di MASTER, dipanggil oleh NODE
service Coordinator {
  rpc Register(RegisterRequest)   returns (RegisterResponse);
  rpc Heartbeat(HeartbeatRequest) returns (HeartbeatResponse);
}

// Berjalan di setiap NODE, dipanggil oleh MASTER
service Worker {
  rpc ProcessImage(ProcessRequest) returns (ProcessResponse);
}
```

```
NODE   ── Register / Heartbeat ──►  MASTER   (Coordinator)
MASTER ── ProcessImage ──────────►  NODE     (Worker)
```

**Alur RPC menurut slide:** klien memanggil *stub* seperti prosedur lokal, stub
melakukan *marshalling* parameter ke pesan, OS mengirimnya, server stub
membongkar dan memanggil prosedur asli **[S03 h.16–17]**. Slide juga membahas *data representation*: mesin bisa berbeda
ukuran tipe data dan urutan byte (little/big-endian), sehingga klien dan server
harus sepakat pada format pesan **[S03 h.28]**. Inilah yang diselesaikan
Protocol Buffers; `protoc` menghasilkan kedua stub itu dari file `.proto`.

**Keputusan:**
- Task dikirim master → node (*push*); registrasi dan heartbeat dimulai node.
- Gambar dikirim **unary**, maksimum **5 MB** per gambar, **20 gambar** per job.
  Batas pesan gRPC dinaikkan eksplisit (~8 MB) karena default 4 MB.
- Format diterima: JPEG dan PNG saja.

---

## 6. Penamaan

Slide: *nama dipakai untuk mengidentifikasi entity secara unik, dan dipetakan ke
lokasinya lewat name resolution* **[S05 h.6]**. Slide mengklasifikasikan sistem
penamaan menjadi tiga kelas: **Flat**, **Structured**, dan **Attribute-based**
**[S05 h.10]**. Bagian ini memetakan ketiganya ke proyek dan **menandai dengan
jujur mana yang benar-benar dibangun dan mana yang hanya analogi**.

| Kelas (slide) | Status di proyek | Ringkasan |
|---|---|---|
| **Flat naming** | ✅ **Diimplementasikan** | Registry node (home-based) + ID job/task |
| **Structured naming** | 🟡 **Ada, dalam bentuk URL REST** | `/api/v1/jobs/{id}/results/{file}` adalah structured name |
| **Attribute-based naming** | ⚪ **Tidak dibangun** | Hanya analogi; dijelaskan kenapa dilewati |

### 6.1 Name, Address, Identifier

Slide membedakan tiga jenis referensi ke sebuah entity **[S05 h.7]**:

| Jenis | Definisi (slide) | Di proyek ini |
|---|---|---|
| **Name** | Deretan bit/karakter yang merujuk entity; boleh mudah dibaca manusia | `node-1`, `master` |
| **Address** | Alamat *access point* entity (contoh slide: **IP + port**) | `192.168.1.11:9000` |
| **Identifier** | Name yang *unik*: merujuk **paling banyak satu** entity, tiap entity punya **paling banyak satu** identifier, dan **tidak pernah dipakai ulang** | `job_id`, `task_id`, ID sesi node |

**Koreksi terhadap draf sebelumnya.** `node-1` bukan *true identifier* menurut
definisi slide: ia bisa dipakai ulang jika laptop restart atau digantikan laptop
lain. Perbaikan sederhana 🟡:

| Entity | Name (mudah dibaca) | Identifier (unik, tak dipakai ulang) |
|---|---|---|
| Node | `node-1` | `node-1` + ID sesi acak, mis. `node-1#a3f9` (dibuat tiap node start) |
| Job | — | `job_{id-acak}` |
| Task | — | `{job_id}-{index}` |

ID sesi membuat sistem bisa membedakan "node-1 yang lama (sudah mati)" dari
"node-1 yang baru menyala", sehingga hasil dari node lama tidak salah dianggap
dari node baru.

### 6.2 Flat naming: registry sebagai home-based approach ✅

Slide menyebut flat name adalah string acak yang **tidak memuat informasi cara
menemukan entity**, dengan empat mekanisme resolusi: *broadcasting*, *forwarding
pointers*, *home-based*, dan *DHT* **[S05 h.12]**. `node-1#a3f9` adalah flat name
(tidak memberi tahu di mana laptopnya), jadi perlu mekanisme resolusi. Model kami
paling mirip **home-based approach** **[S05 h.16]**:

| Konsep slide | Di proyek ini |
|---|---|
| Entity punya **home node** yang statis dan mengetahui alamat terkini | **Master** (alamat tetap, diketahui semua node lewat `--master`) |
| Entity **melapor alamat terbarunya** ke home | Node memanggil `Register` / `Heartbeat` |
| Klien menghubungi home untuk mendapat alamat, lalu menghubungi entity | Scheduler bertanya ke **registry**: `node-2 → 192.168.1.12:9000` |

Alur slide **[S05 h.17]**: entity memperbarui home dengan alamat barunya →
klien bertanya ke home → klien menghubungi entity di alamat itu. Persis alur
`Register` → registry → `ProcessImage`.

**Kelemahan home-based menurut slide dan dampaknya di sini** **[S05 h.18]**:

| Kelemahan (slide) | Kejadian nyata di proyek | Penanganan |
|---|---|---|
| Alamat home permanen; jika entity pindah permanen, overhead naik | Node ganti IP karena DHCP/WiFi | `Register` ulang memperbarui registry |
| Klien bisa memegang **alamat tidak valid** | Scheduler mengirim task ke IP lama | Timeout → reschedule (Bagian 7) |
| Alamat bisa **tidak konsisten** | Dua node mengaku `node-2` | ID sesi membedakan keduanya |
| Home tunggal | Master mati → registry hilang | **SPOF**, diakui di Bagian 2.3 |

**Kenapa bukan mekanisme lain:**

| Mekanisme (slide) | Alasan tidak dipakai |
|---|---|
| Broadcasting | Tidak skalabel, membanjiri jaringan **[S05 h.13]** |
| Forwarding pointers | Untuk entity yang berpindah; rantai panjang rentan putus **[S05 h.14]** |
| DHT / Chord | Butuh hash key, ring logis, finger table **[S05 h.19–24]**; berlebihan untuk 4 node |

> Slide menyebut naming system adalah *middleware yang membantu name
> resolution* **[S05 h.10]**. Registry kami adalah naming system minimal itu.

### 6.3 Structured naming: URL REST 🟡

Slide: *structured name tersusun dari nama sederhana yang mudah dibaca dan
berstruktur menurut konteks; contohnya path file dan URL website*
**[S05 h.31]**. Structured name diorganisasi dalam **name space**, yaitu
*directed graph* dengan *leaf node* (entity) dan *directory node* (menunjuk
node lain) **[S05 h.32]**.

URL REST kita **adalah structured name**, dan resolusinya berjalan dari kiri ke
kanan seperti menelusuri path:

```
/api/v1/jobs/job_7f3a/results/img_03.png
   │    │    │     │       │       │
   │    │    │     │       │       └─ leaf: entity (file hasil)
   │    │    │     │       └───────── directory: kumpulan hasil
   │    │    │     └───────────────── directory: satu job (identifier)
   │    │    └─────────────────────── directory: kumpulan job
   │    └──────────────────────────── versi API
   └───────────────────────────────── akar name space
```

| Konsep slide | Di proyek ini |
|---|---|
| Name space = naming graph | Pohon resource REST (`/jobs`, `/nodes`) |
| Directory node | `/api/v1/jobs`, `/api/v1/jobs/{id}`, `/results` |
| Leaf node | File hasil; detail sebuah node |
| Name resolution | Router `net/http` mencocokkan pola path `{id}` dan `{file}` |
| Mounting: satu name space menyimpan identifier node dari name space lain **[S05 h.52]** | *Tidak dipakai*. Semua resource ada di satu name space milik master |

Ini juga menjawab pertanyaan menarik: **klien tidak pernah memakai `node-1`
atau IP node.** Klien hanya tahu URL master. Nama internal (flat, 6.2) dan nama
publik (structured, 6.3) sengaja dipisah; itu bentuk *transparansi lokasi*.

Slide juga membahas cara mendistribusikan naming graph ke beberapa mesin, yaitu
memetakan sebagian graph ke *master machine* dan sebagian ke *worker machine*
**[S05 h.55]**. Kami **tidak** mendistribusikan name space: seluruhnya di master.
Itu sekali lagi konsekuensi arsitektur Master-Slave dan diakui sebagai
keterbatasan.

### 6.4 Attribute-based naming: tidak dibangun ⚪

Slide: entity dicari lewat atribut, mirip *yellow pages*, contohnya LDAP dengan
pasangan (atribut, nilai) **[S05 h.60–62]**. Slide juga memperingatkan bahwa
pencarian berbasis atribut bisa **sangat mahal** karena mungkin harus memeriksa
semua entity **[S05 h.60]**.

Proyek ini **tidak** memiliki pencarian berdasarkan atribut, dan kami sengaja
tidak menambahkannya: itu penambahan fitur tanpa kebutuhan nyata (melanggar
aturan scope beku). Satu-satunya bentuk yang mendekati adalah *scheduler memilih
node berdasarkan kapasitas atau beban*, tetapi itu **pemilihan**, bukan
**penamaan**, sehingga sebaiknya tidak diklaim sebagai attribute-based naming.

> **Kalau ditanya dosen "mana attribute-based naming di proyekmu?"** Jawaban
> jujur: *"Tidak ada. Kami menyadari kelas ini dan sengaja tidak
> mengimplementasikannya karena pencarian atribut mahal dan tidak dibutuhkan
> untuk 4 node."*

## 7. Fault Tolerance dan Semantik RPC

Bagian ini paling sering dinilai. Semuanya disandarkan pada slide RPC.

### 7.1 Model kegagalan

Slide mengklasifikasikan kegagalan server: **crash**, **omission** (tidak
merespons), **timing**, **response**, dan **arbitrary/Byzantine** **[S03 h.42]**.
Yang kami tangani:

| Jenis (slide) | Contoh di proyek | Penanganan |
|---|---|---|
| **Crash** | Proses/laptop node mati | Heartbeat berhenti → node ditandai mati → task dijadwalkan ulang |
| **Omission** | Node hidup tetapi tidak membalas | Timeout → task dijadwalkan ulang |
| **Timing** | Node terlalu lambat | Timeout; hasil terlambat diabaikan |
| Response / Arbitrary | Hasil gambar salah/rusak | **Tidak ditangani** (di luar scope) |

### 7.2 Timeout

Slide: untuk permintaan atau balasan yang hilang, RPC memakai timeout; setelah
timeout, bisa langsung gagal atau mengirim ulang **[S03 h.43]**. Slide juga
jujur bahwa memilih nilai timeout itu sulit: *"at best, use empirical/theoretical
statistics; at worst, no good value exists"*. Karena itu nilai timeout kami
adalah **titik awal yang harus diukur**, bukan angka pasti (Bagian 9).

### 7.3 Idempotency dan semantik panggilan

Slide: operasi yang bisa dijalankan berulang dengan efek sama disebut
**idempotent** **[S03 h.44]**. Semantik RPC ditentukan kombinasi tiga hal
**[S03 h.52]**:

| Retransmit request | Duplicate filtering | Re-execute / retransmit reply | Semantik |
|---|---|---|---|
| Tidak | — | — | Maybe |
| Ya | Tidak | Re-execute procedure | **At-least-once** |
| Ya | Ya | Retransmit reply | At-most-once |

Slide juga menyatakan **idealnya exactly-once**. Ringkasan penanganan di slide
**[S03 h.53]**: *jika prosedur idempotent, server cukup mengeksekusinya
(at-least-once); jika tidak, server harus memeriksa apakah sudah pernah
dieksekusi dan mengembalikan hasil tersimpan (at-most-once)*.

**Keputusan kami:** `ProcessImage` **idempotent** (gambar sama + parameter sama =
hasil sama), maka kami memakai **at-least-once**. Ini pilihan yang sah menurut
slide, tanpa perlu menyimpan riwayat hasil.

> **Rumusan aman untuk laporan:** *"Sistem menggunakan **at-least-once
> execution**. Dampak duplikasi diminimalkan melalui **idempotent processing**
> dan mekanisme **first-result-wins**."* Hindari menulis "exactly-once".

### 7.4 Kegagalan RPC tidak membuktikan node mati

Timeout hanya berarti balasan tidak datang; node bisa saja masih bekerja. Karena
itu dua node bisa mengerjakan gambar yang sama (kondisi yang persis dijelaskan
slide untuk retransmisi **[S03 h.44]**). Aturan scheduler:

1. Task ID deterministik `{job_id}-{index}`.
2. Status hanya maju: `PENDING → RUNNING → DONE / FAILED`.
3. Hasil untuk task yang sudah `DONE` **diabaikan** (first-result-wins).
4. Hasil ditulis **atomik**: tulis ke file sementara lalu `rename`.
5. Task dijadwalkan ulang **maksimal 3 kali**, lalu `FAILED`.

Alasan angka 3: slide *careful file transfer* menyebut biasanya 1 retry jika
kegagalan jarang, dan **3 retry mungkin menandakan ada bagian sistem yang perlu
diperbaiki** **[S03 h.60]**. Jadi 3 adalah batas wajar sebelum menyerah. 🟡

### 7.5 Prinsip end-to-end

Slide mengutip *End-to-End Arguments in System Design* (Saltzer dkk.): walaupun
gRPC berjalan di atas TCP yang sudah meretransmisi paket, **aplikasi tetap
harus punya jaminan reliabilitas sendiri** **[S03 h.55, 61]**. Itu alasan kami
menambahkan timeout, retry, dan reschedule di lapisan aplikasi, bukan
mengandalkan TCP. TCP hanya menghapus ancaman paket hilang, bukan ancaman node
mati atau hasil korup.

### 7.6 Deteksi kegagalan

- Node mengirim heartbeat tiap **2 detik**; master menandai mati jika tidak ada
  heartbeat **6 detik** (3× interval). 🟡 Angka awal, wajib diukur.
- Node yang menyala kembali cukup `Register` ulang (Bagian 6).
- **Jika semua node mati**, master mengerjakan semuanya sendiri (*graceful
  degradation*) dan UI menampilkan peringatan.

---

## 8. Tech Stack dan Library

| Lapisan | Teknologi |
|---|---|
| Backend | Go: lantai **`go 1.25.0`** di `go.mod`, dipasang **Go 1.26.x** |
| RPC | gRPC + Protocol Buffers (gRPC-Go **≥ v1.79.3**) |
| REST | `net/http` (routing method+wildcard sejak Go 1.22) |
| Frontend | Vue 3 + Vite (Composition API) |
| Embed UI | `//go:embed` → tetap **single binary** |
| Build | Makefile |

### 8.1 Kebijakan versi Go

| Hal | Keputusan | Dasar (dari sumber resmi) |
|---|---|---|
| Baris `go` di `go.mod` | `go 1.25.0` | gRPC-Go mensyaratkan minimum 1.25 sejak v1.81.0 |
| Yang dipasang tim | **Go 1.26.x** | Didukung Go dan gRPC-Go (mendukung dua rilis mayor terbaru); sudah matang |
| Go 1.27 | Boleh, bukan prioritas | Baru rilis 19 Agu 2026; patch pertama baru 1 Sep |
| Go 1.25 | Cukup untuk membangun, tidak disarankan dipasang | Di luar dukungan formal setelah 1.27 rilis |

⚠️ Lantai `1.25` **bisa naik** di rilis gRPC-Go berikutnya (baris `go` adalah
minimum yang diwajibkan dependensi). Buktikan lantai dengan:

```bash
go mod init distapi
go get google.golang.org/grpc@latest google.golang.org/protobuf@latest golang.org/x/sync@latest
go mod tidy
grep -E '^(go|toolchain) ' go.mod   # jika naik dari 1.25.0, ada dependensi yang butuh lebih baru
```

gRPC ≥ v1.79.3 dipilih karena CVE-2026-33186 (bypass otorisasi via header
`:path`) diperbaiki di sana; proyek ini memakai token sederhana sehingga tidak
terdampak langsung, tetapi versi aman tetap dipakai.

### 8.2 Dependensi langsung: hanya 3

| Library | Alasan masuk |
|---|---|
| `google.golang.org/grpc` | RPC wajib muncul (standar industri) |
| `google.golang.org/protobuf` | Kontrak `.proto` (dependensi wajib gRPC) |
| `golang.org/x/sync` (`errgroup`) | Scatter–gather paralel dengan propagasi error dan cancel; `WaitGroup.Go` (Go 1.25) tidak menangani error/context |

Sisanya stdlib: `net/http`, `encoding/json`, `log/slog`, `context`, `embed`,
`image/*`, `sync`, `os/signal`, `flag`, `testing`, `httptest`.

**Sengaja tidak dipakai:** gin/echo (net/http cukup), viper/cobra, zap/logrus
(`slog` cukup), testify, uuid, validator, backoff, ORM/database.

**Opsional:** `disintegration/imaging` bila hasil resize stdlib terlalu kasar.

### 8.3 Tools pengembangan

`protoc` + `protoc-gen-go` + `protoc-gen-go-grpc` hanya untuk yang mengubah
kontrak. Hasil generate (`gen/`) **di-commit**, sehingga anggota lain cukup
`go build`. Node.js hanya di mesin yang membangun frontend.

⚠️ Dokumentasi `errgroup` dan `protobuf` belum sempat dibaca langsung; kesesuaian
dengan lantai 1.25 adalah inferensi, dibuktikan lewat `go mod tidy` di atas.

---

## 9. Konfigurasi dan Menjalankan

**Standar 12-Factor:** prioritas `flag > environment variable > default`. Tanpa
file config, jadi tidak ada file yang perlu dikopi ke 4 laptop.

### 9.1 Menjalankan

```bash
# LAPTOP 1 — master (DUA server: HTTP untuk REST/UI, gRPC untuk cluster)
./distapi --mode=master --http-port=8080 --grpc-port=9000 --token=demo123

# LAPTOP 2 — node-1
./distapi --mode=node --node-id=node-1 --grpc-port=9000 \
          --master=192.168.1.10:9000 --advertise=192.168.1.11:9000 --token=demo123

# LAPTOP 3 — node-2 : --node-id=node-2 --advertise=192.168.1.12:9000
# LAPTOP 4 — node-3 : --node-id=node-3 --advertise=192.168.1.13:9000
```

Ketiga node boleh memakai port sama karena mesinnya berbeda. IP di atas hanya
contoh. Node harus mengiklankan **IP yang bisa dijangkau**, bukan `localhost`
atau `0.0.0.0`.

### 9.2 Parameter

| Parameter | Flag | Default | Mode |
|---|---|---|---|
| Mode | `--mode` | wajib | keduanya |
| Port HTTP | `--http-port` | `8080` | master |
| Port gRPC | `--grpc-port` | `9000` | keduanya |
| Node ID | `--node-id` | hostname | node |
| Alamat master | `--master` | wajib di node | node |
| Alamat diiklankan | `--advertise` | auto-detect | node |
| Interval heartbeat | `--heartbeat-interval` | `2s` | node |
| Batas node mati | `--node-timeout` | `6s` | master |
| Timeout task | `--task-timeout` | `30s` | master |
| Maks percobaan ulang | `--max-retries` | `3` | master |
| Jumlah worker | `--workers` | node: `NumCPU`, master: `max(1, NumCPU/2)` | keduanya |
| Direktori data | `--data-dir` | `./data` | master |
| Batas gambar | `--max-image-mb` / `--max-images` | `5` / `20` | master |
| Retensi job | `--job-ttl` | `1h` | master |
| Token cluster | `--token` | wajib | keduanya |
| Log level | `--log-level` | `info` | keduanya |

Setiap parameter juga bisa diisi via env `DISTAPI_<NAMA>`. Konfigurasi divalidasi
sekali saat startup; salah konfigurasi → berhenti dengan pesan jelas (*fail fast*).

Master jumlah worker-nya dibatasi karena ia juga melayani REST, menerima
heartbeat, dan memantau node. Jika CPU master penuh, heartbeat node bisa
terlambat diproses sehingga **node sehat salah ditandai mati**. 🟡

---

## 10. Struktur Project dan Pembagian Kerja

Konvensi `cmd/` + `internal/` (panduan go.dev *Organizing a Go module*). Kami
tidak memakai `pkg/` maupun struktur berlapis yang tidak dibutuhkan.

```
distapi/
├── cmd/distapi/main.go       # parse config, pilih mode, wiring, graceful shutdown
├── proto/cluster.proto       # kontrak gRPC (Coordinator + Worker)
├── gen/                      # hasil generate protobuf (di-commit)
├── internal/
│   ├── config/               # flag + env, validasi
│   ├── api/                  # REST handler + serve frontend embed
│   ├── scheduler/            # pecah job, scatter-gather, retry/reschedule
│   ├── registry/             # daftar node, heartbeat, name resolution
│   ├── rpc/                  # gRPC server & client
│   ├── worker/               # resize + grayscale (dipakai master & node)
│   └── storage/              # file sementara (upload + hasil)
├── web/                      # Vue 3 + Vite (dist/ di-embed ke binary)
├── Makefile
├── go.mod
└── README.md
```

### 10.1 Pembagian kerja (minim konflik merge) 🟡

| Anggota | Tanggung jawab | Folder utama |
|---|---|---|
| **A** | Scheduler, registry, fault tolerance | `scheduler/`, `registry/` |
| **B** | gRPC: proto, server, client, heartbeat | `proto/`, `rpc/`, `gen/` |
| **C** | REST API, storage, config, main, **integrator** | `api/`, `storage/`, `config/`, `cmd/` |
| **D** | Frontend Vue + worker resize/grayscale | `web/`, `worker/` |

Pembagian di atas hanya usulan struktur; **tukar peran sesuai kemampuan
masing-masing** (tidak ada asumsi tentang siapa yang mahir apa). Yang tetap:
kontrak (`.proto` dan struct REST) disepakati **lebih dulu**, dan satu orang
menjadi **integrator** yang menjaga `main` selalu bisa dibuild.

Aturan sederhana: satu cabang `main` yang selalu hijau (`go build ./...` dan
`go test ./...`); cabang fitur per orang, digabung lewat pull request;
**integrasi kerangka di minggu pertama** (master dan node saling `Register` dan
bertukar satu RPC dummy) sebelum ada fitur nyata.

---

## 11. Praktik Engineering

Semua murah dan bernilai; masing-masing terhubung ke sebuah konsep kuliah.

| Praktik | Implementasi | Terkait |
|---|---|---|
| Structured logging | `log/slog`, sertakan `job_id`, `task_id`, `node_id` | Debug lintas 4 laptop |
| Graceful shutdown | `signal.NotifyContext`, `http.Server.Shutdown`, `GracefulStop` | Node pamit ke master |
| `context.Context` | Di semua panggilan antar-node | Timeout **[S03 h.43]** |
| Timeout di semua panggilan jaringan | Deadline per RPC; `ReadHeaderTimeout` di server | Jaringan tak bisa dipercaya |
| Error wrapping | `fmt.Errorf("...: %w", err)` | — |
| Dependency injection sederhana | Konstruktor + interface kecil | Testabilitas |
| Testing | Unit test registry/worker/scheduler; `httptest`; `go test -race` | Banyak goroutine |
| Keamanan dasar | Shared secret token (`X-Cluster-Token` / metadata gRPC) | Cegah node liar |
| Idempotency | Task ID deterministik, hasil atomik | **[S03 h.44, 53]** |
| Health check | `/healthz` + heartbeat | Failure detection |

---

## 12. Rencana Demo dan Benchmark

### 12.1 Alur demo (tujuh langkah, tiap langkah terhubung ke materi)

| # | Yang ditunjukkan | Konsep |
|---|---|---|
| 1 | Topologi: 1 master + 3 worker di 4 laptop | Master-Slave **[S04]**, horizontal scaling |
| 2 | Jalankan semua binary; 3 node `Register` ke master; UI menampilkan 4 node | Naming / home-based **[S05 h.16]** |
| 3 | Unggah 12–20 gambar lewat web (hanya memanggil REST) | RESTful API **[S06]** |
| 4 | Buka log 4 laptop bersamaan: `task-01 → node-1`, `task-02 → node-2`, … | Scatter–gather, RPC **[S03]** |
| 5 | **Cabut satu laptop dari WiFi di tengah proses** → heartbeat hilang → timeout → task dipindah → job selesai | Failure, timeout, idempotency **[S03 h.42–44]** |
| 6 | Nyalakan node lagi → `Register` ulang, name→address diperbarui | Name resolution **[S05]** |
| 7 | Benchmark 1, 2, 3, 4 worker | Amdahl **[S02 h.16–18]** |

### 12.2 Benchmark: menjelaskan mengapa speedup tidak linear

Slide: `Speedup = T1 / Tm` **[S02 h.15]**, dan Amdahl `1 / (s + (1−s)/m)`. Contoh
slide: 80% paralel di 4 mesin → **2,5×**, bukan 4× **[S02 h.17]**.

Slide menegaskan Amdahl terlalu sederhana; di dunia nyata ada **communication
overhead** dan **load imbalance** **[S02 h.18]**. Panduan slide **[S02 h.19–20]**
langsung menjadi checklist analisis kami:

| Panduan slide | Diukur / dijelaskan di proyek |
|---|---|
| Fraksi program yang bisa diparalelkan | Waktu resize (paralel) vs upload + gabung hasil (serial di master) |
| Efisiensi dan utilisasi tiap mesin | CPU tiap laptop saat benchmark |
| Heterogenitas antar-mesin | Spesifikasi laptop berbeda → node cepat menunggu yang lambat |
| Latensi dan bandwidth jaringan | `Waktu kirim = latensi + ukuran/laju` **[S02 h.22]** |

Rekam: waktu total, waktu per node, jumlah task per node, ukuran data terkirim.
Sajikan tabel dan grafik speedup; bandingkan dengan kurva Amdahl.

**Jangan berharap hasil linear.** Justru ketidaklinearan itu bahan analisis.

---

## 13. Keputusan Terbuka dan Risiko

### 13.1 Harus dijawab sebelum menulis kode

| # | Pertanyaan | Kenapa penting | Bagaimana |
|---|---|---|---|
| **D1** | Apakah 4 laptop benar-benar bisa saling terhubung (semua arah)? | WiFi kampus sering menerapkan *client isolation*; firewall Windows memblokir koneksi masuk. Ini satu-satunya risiko yang tak bisa diperbaiki dengan kode | Tes TCP antar semua laptop hari ini. Cadangan: hotspot HP |
| **D2** | Apa rubrik, deadline, dan format deliverable dari dosen? | Desain mengasumsikan rubrik; belum pernah dilihat | Tanya dosen. ⚠️ Slide S04 h.3 memuat "Design report due September 22 / PS2 due October 1"; **belum diketahui apakah berlaku untuk kelompok ini** |
| **D3** | Apakah gRPC diterima sebagai RPC? | Tidak ada larangan di slide, tetapi *tidak adanya larangan bukan konfirmasi* | Tanya singkat ke dosen. Cadangan: JSON-RPC lewat HTTP (hanya `internal/rpc` berubah) |

### 13.2 Keputusan desain yang sudah diambil 🟡

| Topik | Keputusan | Alasan singkat |
|---|---|---|
| Pembagian task | 1 gambar = 1 task | Sederhana; idempotent alami. Demo memakai 12–20 gambar |
| Arah komunikasi | Node → master (register/heartbeat), master → node (task) | Master tak perlu tahu IP di awal |
| Pengiriman gambar | gRPC unary, maks 5 MB | Streaming baru perlu jika gambar lebih besar |
| Master restart | State hilang, dicatat sebagai keterbatasan | Persistensi = tier baru, di luar scope |
| Penyimpanan hasil | Direktori sementara di master, dihapus setelah 1 jam dan saat startup | Node stateless |
| Dispatch task | Mulai dari round-robin; naik ke dispatch dinamis jika waktu ada | Node cepat tak menunggu node lambat **[S02 h.19]** |
| Jika jaringan hanya satu arah | Beralih ke model *pull* (node mengambil task) | Lebih tahan firewall |

### 13.3 Prioritas jika waktu tidak cukup

Potong **dari bawah**, tidak pernah dari atas:

1. **Master + 3 node berjalan di 4 laptop dan saling bertukar RPC**
2. **Scatter–gather nyata**: gambar diproses di semua node
3. **Fault tolerance**: node mati → task dipindah → job selesai
4. UI Vue yang menampilkan progres dan node
5. Benchmark speedup
6. Kerapian tambahan (CI, dll.)

### 13.4 Keterbatasan yang diakui

Master = SPOF; state job di memori; kegagalan *response/arbitrary* tidak
ditangani; tidak ada TLS. Semuanya masuk bagian *future work* di laporan.

---

## 14. Peta Materi ke Proyek

Tiap baris menunjukkan bahwa konsep kuliah **benar-benar bisa didemokan**, bukan
sekadar disebut.

| Pertemuan | Konsep kuliah | Bukti di proyek | Bagian |
|---|---|---|---|
| **02 Networking** | Distribution gain, Amdahl, latensi/bandwidth, overhead komunikasi, load imbalance | Benchmark 1–4 node; analisis speedup non-linear | 12 |
| **03 RPC** | Stub & marshalling, failure model, timeout, idempotent, call semantics, end-to-end | gRPC; timeout, retry ≤3, reschedule; at-least-once | 5, 7 |
| **04 Arsitektur** | Master-Slave, SPOF, tiering, layering (Platform/Middleware/Applications) | Master + 3 worker; variasi 3-tier; gRPC sebagai middleware | 2, 3, 4 |
| **05 Penamaan** | Name/Address/Identifier, name resolution, home-based | Registry `node → IP:port`; master sebagai home; ID sesi | 6 |
| **06 RESTful API** | Demo aplikasi web dengan RESTful API | `/api/v1/jobs`, 202 Accepted, Vue | 5.1 |

### Yang belum terverifikasi

- ⚠️ Dokumentasi `errgroup` dan `protobuf` (situs `pkg.go.dev` tidak bisa
  diakses saat riset); kesesuaian dengan Go 1.25 adalah inferensi.
- ⚠️ Dukungan eksplisit gRPC-Go terhadap Go 1.27: README hanya menyebut "dua
  rilis terbaru".
- ⚠️ **Semua kode**: belum ada yang ditulis, dikompilasi, atau diuji.
- Klaim "REST di tepi, gRPC di dalam" berasal dari praktik umum/artikel, bukan
  slide. Untuk laporan, kutip slide dan paper MapReduce (Dean & Ghemawat, 2004)
  untuk klaim fundamental.