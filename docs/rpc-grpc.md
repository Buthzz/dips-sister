# RPC dan gRPC — distapi

## 1. Remote Procedure Call (RPC)

RPC adalah mekanisme yang memungkinkan proses memanggil prosedur di mesin lain seolah-olah lokal. Abstraksi ini menyembunyikan detail jaringan dari programmer.

```mermaid
sequenceDiagram
    participant Caller as Pemanggil (Master)
    participant Stub as Client Stub
    participant Net as TCP / HTTP-2
    participant Skel as Server Skeleton
    participant Impl as Implementasi (Node)

    Caller->>Stub: ProcessImage(req)
    Note over Stub: Marshalling (Protobuf encode)
    Stub->>Net: kirim bytes via TCP
    Net->>Skel: terima bytes
    Note over Skel: Unmarshalling (Protobuf decode)
    Skel->>Impl: ProcessImage(req)
    Impl-->>Skel: return resp
    Note over Skel: Marshalling resp
    Skel-->>Net: kirim bytes
    Net-->>Stub: terima bytes
    Note over Stub: Unmarshalling resp
    Stub-->>Caller: return resp
```

### Tantangan RPC vs Fungsi Lokal

| Aspek | Fungsi Lokal | RPC |
|-------|-------------|-----|
| Latensi | Nanosecond | Milidetik (network RTT) |
| Kegagalan | Tidak ada | Timeout, network error, node crash |
| Serialisasi | Tidak perlu | Wajib (marshalling/unmarshalling) |
| Partial failure | Tidak ada | Bisa terjadi (caller tidak tahu apakah callee mati) |

Partial failure adalah tantangan utama RPC: caller tidak bisa membedakan apakah callee gagal *sebelum* atau *sesudah* memproses request. Ini yang mendorong semantik **at-least-once** di distapi.

## 2. Mengapa gRPC (bukan REST untuk Internal)?

gRPC dipilih untuk komunikasi *internal* (master ↔ node) dengan alasan:

| Aspek | REST + JSON | gRPC + Protobuf |
|-------|------------|-----------------|
| Encoding gambar (binary) | Base64 (+33% ukuran) | Binary langsung |
| Overhead header | HTTP/1.1 teks | HTTP/2 compressed |
| Code generation | Manual | Otomatis dari `.proto` |
| Type safety | Runtime | Compile-time |
| Untuk gambar 5 MB | ~6.6 MB via base64 | 5 MB langsung |

REST tetap dipakai untuk antarmuka *eksternal* (klien → master) karena lebih universal dan mudah di-test dengan `curl`.

## 3. Protocol Buffers — Definisi Service

File [`proto/cluster.proto`](../proto/cluster.proto) mendefinisikan dua service dengan arah panggilan yang berbeda:

```protobuf
// Coordinator: berjalan di MASTER, dipanggil NODE
// Arah: NODE → MASTER
service Coordinator {
  rpc Register(RegisterRequest) returns (RegisterResponse);
  rpc Heartbeat(HeartbeatRequest) returns (HeartbeatResponse);
}

// Worker: berjalan di setiap NODE, dipanggil MASTER
// Arah: MASTER → NODE
service Worker {
  rpc ProcessImage(ProcessRequest) returns (ProcessResponse);
}
```

Pemisahan dua service kritis karena arah panggilan berlawanan:

```mermaid
flowchart LR
    Node["Node\n(client Coordinator)\n(server Worker)"]
    Master["Master\n(server Coordinator)\n(client Worker)"]

    Node -->|"Register / Heartbeat"| Master
    Master -->|"ProcessImage"| Node
```

## 4. Message Types

```mermaid
flowchart TD
    RegisterRequest["RegisterRequest\n- node_id: string\n- session_id: string\n- advertise_addr: string\n- capacity: int32"]
    RegisterResponse["RegisterResponse\n- accepted: bool\n- message: string"]

    HeartbeatRequest["HeartbeatRequest\n- node_id: string\n- session_id: string\n- active_tasks: int32"]
    HeartbeatResponse["HeartbeatResponse\n- ok: bool"]

    ProcessRequest["ProcessRequest\n- task_id: string\n- job_id: string\n- image_index: int32\n- image_data: bytes\n- filename: string\n- resize_width/height: int32\n- grayscale: bool"]
    ProcessResponse["ProcessResponse\n- task_id: string\n- success: bool\n- result_data: bytes\n- filename: string\n- error: string\n- duration_ms: int64"]

    RegisterRequest --> RegisterResponse
    HeartbeatRequest --> HeartbeatResponse
    ProcessRequest --> ProcessResponse
```

## 5. Autentikasi via gRPC Metadata

Karena tidak menggunakan TLS (DESIGN.md §13.4 — *future work*), autentikasi dilakukan dengan **shared token** yang disisipkan ke metadata setiap RPC call.

```mermaid
sequenceDiagram
    participant Client as Node (gRPC Client)
    participant Intercept as Client Interceptor
    participant Wire as Jaringan
    participant ServIntercept as Server Interceptor
    participant Handler as Handler Master

    Client->>Intercept: Register(req)
    Note over Intercept: Inject metadata:\nx-cluster-token: demo123
    Intercept->>Wire: RPC + metadata
    Wire->>ServIntercept: RPC + metadata
    Note over ServIntercept: Ekstrak dan validasi token
    alt Token valid
        ServIntercept->>Handler: Register(req)
        Handler-->>Client: RegisterResponse
    else Token salah
        ServIntercept-->>Client: status.Error(Unauthenticated)
    end
```

Implementasi:
```go
// Server interceptor — validasi sebelum handler
func UnaryTokenServerInterceptor(token string) grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo,
        handler grpc.UnaryHandler) (any, error) {
        md, _ := metadata.FromIncomingContext(ctx)
        if vals := md.Get("x-cluster-token"); len(vals) == 0 || vals[0] != token {
            return nil, status.Error(codes.Unauthenticated, "token tidak valid")
        }
        return handler(ctx, req)
    }
}
```

## 6. Sequence Startup Node

```mermaid
sequenceDiagram
    participant N as Node (binary start)
    participant GS as gRPC Worker Server
    participant M as Master (Coordinator)
    participant HL as Heartbeat Loop

    N->>GS: Mulai Worker server di :9000
    N->>M: Register(node_id, session_id, advertise, capacity)
    M-->>N: {accepted: true}

    Note over M: hookPendaftaran: DialNode balik ke node
    M->>N: DialNode(advertise_addr) — buka koneksi balik

    N->>HL: Mulai goroutine heartbeat
    loop Setiap 2 detik
        HL->>M: Heartbeat(node_id, session_id, active_tasks)
        M-->>HL: {ok: true}
    end
```

## 7. Semantik Eksekusi: At-Least-Once

gRPC unary digunakan (bukan streaming). Timeout per task: 30 detik (konfigurabel). Jika timeout tercapai atau node mati, task di-retry ke node lain — menghasilkan semantik **at-least-once**.

Task yang sama bisa dieksekusi dua kali bersamaan (stale RUNNING + retry baru). Ini aman karena `worker.Process()` bersifat deterministik dan `storage.SaveResult()` menggunakan atomic rename — **first-result-wins**.

> Lihat [`fault-tolerance.md`](fault-tolerance.md) untuk detail mekanisme retry dan reschedule.
