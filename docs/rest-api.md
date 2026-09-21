# Spesifikasi RESTful API — distapi

Dokumen ini mendefinisikan kontrak antarmuka eksternal (*External Client Interface*) dari node **Master**. Desain API mengikuti konvensi **RESTful Architecture** dengan identifikasi resource berbasis kata benda, pemanfaatan metode HTTP semantik, pemrosesan asinkron via status `202 Accepted`, dan format amplop error JSON yang seragam.

## 1. Daftar Endpoint Publik

| Metode | Jalur URI | Deskripsi Operasional | Status Berhasil |
| :--- | :--- | :--- | :--- |
| `GET` | `/healthz` | Pemeriksaan kesehatan proses Master (*Liveness Probe*) | `200 OK` |
| `POST` | `/api/v1/jobs` | Membuat batch job baru (Unggah multipart) | `202 Accepted` |
| `GET` | `/api/v1/jobs` | Mengambil seluruh daftar histori job | `200 OK` |
| `GET` | `/api/v1/jobs/{id}` | Mengambil status dan detail progres task sebuah job | `200 OK` |
| `DELETE` | `/api/v1/jobs/{id}` | Membatalkan job dan menghapus seluruh berkas terkait | `204 No Content` |
| `GET` | `/api/v1/jobs/{id}/results/{file}` | Mengunduh hasil olahan citra biner secara streaming | `200 OK` |
| `GET` | `/api/v1/nodes` | Memeriksa topologi dan status kluster node | `200 OK` |

## 2. Rincian dan Contoh Permintaan

### `GET /healthz`
Pemeriksaan ketersediaan layanan Master tanpa dependensi jaringan eksternal.

* **Response Body:**
  ```json
  {
    "status": "ok"
  }
  ```

### `POST /api/v1/jobs`
Menerima kumpulan berkas citra mentah untuk didistribusikan ke kluster worker.

* **Content-Type:** `multipart/form-data`
* **Form Fields:**
  * `images` (berulang, binary file): Berkas citra (Format: `.jpg`, `.jpeg`, `.png`. Maksimal 5 MB/berkas, maksimal 20 berkas/job).
  * `options` (opsional, string JSON): Konfigurasi transformasi citra.
    ```json
    {
      "grayscale": true,
      "resize_width": 800,
      "resize_height": 800
    }
    ```
* **Contoh Pemanggilan PowerShell:**
  ```powershell
  curl.exe -X POST http://192.168.1.10:8080/api/v1/jobs `
    -F "images=@C:\foto1.jpg" `
    -F "images=@C:\foto2.png" `
    -F 'options={"grayscale":true,"resize_width":800,"resize_height":800}'
  ```
* **Response `202 Accepted`:**
  ```json
  {
    "job_id": "job_3f92d81a7b4c2e10",
    "status": "PENDING",
    "tasks": 2
  }
  ```

### `GET /api/v1/jobs`
Mengambil ringkasan seluruh job yang tersimpan di memori Master, diurutkan dari yang terbaru.

* **Response `200 OK`:**
  ```json
  [
    {
      "id": "job_3f92d81a7b4c2e10",
      "status": "DONE",
      "total": 2,
      "done": 2,
      "failed": 0,
      "created_at": "2026-09-21T20:15:30.124+07:00"
    }
  ]
  ```

### `GET /api/v1/jobs/{id}`
Memeriksa status siklus hidup job dan rincian alokasi setiap task pada node worker.

* **Parameter Jalur:** `id` — ID unik job (contoh: `job_3f92d81a7b4c2e10`).
* **Response `200 OK`:**
  ```json
  {
    "id": "job_3f92d81a7b4c2e10",
    "status": "DONE",
    "total": 2,
    "done": 2,
    "failed": 0,
    "created_at": "2026-09-21T20:15:30.124+07:00",
    "tasks": [
      {
        "id": "job_3f92d81a7b4c2e10-000",
        "filename": "foto1.jpg",
        "status": "DONE",
        "assigned_to": "node-1",
        "retries": 0,
        "duration_ms": 42,
        "done_at": "2026-09-21T20:15:30.850+07:00"
      },
      {
        "id": "job_3f92d81a7b4c2e10-001",
        "filename": "foto2.png",
        "status": "DONE",
        "assigned_to": "node-2",
        "retries": 0,
        "duration_ms": 58,
        "done_at": "2026-09-21T20:15:30.910+07:00"
      }
    ]
  }
  ```

### `GET /api/v1/jobs/{id}/results/{file}`
Mengunduh berkas biner hasil pemrosesan citra secara langsung.

* **Parameter Jalur:**
  * `id`: ID unik job.
  * `file`: Nama berkas asli (contoh: `foto1.jpg`).
* **Response Headers:**
  * `Content-Type`: `image/jpeg` atau `image/png`
  * `Content-Disposition`: `attachment; filename="foto1.jpg"`
* **Contoh PowerShell:**
  ```powershell
  Invoke-WebRequest -Uri "http://192.168.1.10:8080/api/v1/jobs/job_3f92d81a7b4c2e10/results/foto1.jpg" `
    -OutFile "C:\hasil_foto1.jpg"
  ```

### `DELETE /api/v1/jobs/{id}`
Membatalkan job yang sedang berjalan dan menghapus direktori berkas terkait secara permanen dari storage Master.

* **Response `204 No Content`** (Badan respons kosong).

### `GET /api/v1/nodes`
Memeriksa status liveness seluruh node yang pernah mendaftar pada Registry Master.

* **Response `200 OK`:**
  ```json
  [
    {
      "node_id": "node-1",
      "status": "alive",
      "addr": "192.168.1.11:9000",
      "active_tasks": 0,
      "capacity": 8,
      "last_heartbeat": "2026-09-21T20:16:02.411+07:00"
    },
    {
      "node_id": "node-2",
      "status": "dead",
      "addr": "192.168.1.12:9000",
      "active_tasks": 0,
      "capacity": 8,
      "last_heartbeat": "2026-09-21T20:15:20.102+07:00"
    }
  ]
  ```

## 3. Format Respons Kesalahan (Error Envelope)

Seluruh respons kegagalan (`4xx` dan `5xx`) mengembalikan amplop JSON baku:

```json
{
  "error": "deskripsi detail penyebab kegagalan"
}
```

| Kode HTTP | Skenario Pemicu |
| :--- | :--- |
| `400 Bad Request` | Body multipart tidak valid, field `images` kosong, atau jumlah gambar > batas maksimum |
| `404 Not Found` | ID Job tidak ditemukan, atau berkas hasil olahan belum tersedia / sudah dihapus |
| `413 Request Entity Too Large` | Ukuran satu berkas gambar melebihi batas konfigurasi (default 5 MB) |
| `415 Unsupported Media Type` | Ekstensi berkas bukan `.jpg`, `.jpeg`, atau `.png` |
| `500 Internal Server Error` | Kegagalan I/O disk Master atau galat internal scheduler |
