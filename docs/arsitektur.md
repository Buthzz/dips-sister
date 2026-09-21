# Arsitektur Sistem — distapi

## 1. Pola Arsitektur: Master-Slave

distapi mengimplementasikan pola **Master-Slave** (DESIGN.md §3). Satu node bertindak sebagai koordinator terpusat (*master*), sisanya sebagai worker (*slave/node*). Pola ini dipilih karena sifat tugas yang **embarrassingly parallel**: setiap gambar dapat diproses secara independen tanpa komunikasi antar node.

```mermaid
flowchart TD
    Client["Klien\n(browser / curl)"]

    subgraph Master["Master — Laptop 1"]
        HTTPAPI["REST API\n:8080"]
        Sched["Scheduler\nScatter-Gather"]
        Reg["Registry\nHome-Based Naming"]
        CoordSrv["gRPC Coordinator\n:9000"]
        WorkerSrv["gRPC Worker\n(master-local fallback)"]
        Store["Storage\nFile System"]
    end

    subgraph Node1["Node-1 — Laptop 2"]
        WS1["gRPC Worker\n:9000"]
        Proc1["Image Processor\nResize + Grayscale"]
    end

    subgraph Node2["Node-2 — Laptop 3"]
        WS2["gRPC Worker\n:9000"]
        Proc2["Image Processor"]
    end

    subgraph Node3["Node-3 — Laptop 4"]
        WS3["gRPC Worker\n:9000"]
        Proc3["Image Processor"]
    end

    Client -->|"POST /jobs\nGET /jobs/{id}"| HTTPAPI
    HTTPAPI --> Sched
    Sched --> Reg
    Sched -->|"ProcessImage (gRPC)"| WS1
    Sched -->|"ProcessImage (gRPC)"| WS2
    Sched -->|"ProcessImage (gRPC)"| WS3
    Sched -->|"fallback"| WorkerSrv
    WS1 --> Proc1
    WS2 --> Proc2
    WS3 --> Proc3
    Sched --> Store
    Node1 -->|"Register + Heartbeat (gRPC)"| CoordSrv
    Node2 -->|"Register + Heartbeat (gRPC)"| CoordSrv
    Node3 -->|"Register + Heartbeat (gRPC)"| CoordSrv
    CoordSrv --> Reg
```

## 2. Alasan Pemilihan Master-Slave

| Kriteria | Master-Slave | Peer-to-Peer | Leader Election |
|----------|-------------|--------------|-----------------|
| Kompleksitas implementasi | Rendah ✅ | Tinggi | Tinggi |
| Cocok untuk independent task | Ya ✅ | Ya | Ya |
| SPOF | Ya ⚠️ | Tidak | Tidak |
| Kebutuhan konsensus | Tidak ✅ | Ya | Ya |
| Sesuai scope tugas (4 laptop) | Ya ✅ | Overkill | Overkill |

Untuk demo 4 laptop tanpa DB dan tanpa orchestration platform, Master-Slave adalah pilihan yang paling proporsional.

## 3. Single Binary, Dual Mode

```mermaid
flowchart LR
    Binary["distapi.exe"]
    FlagMaster["--mode=master"]
    FlagNode["--mode=node"]

    Binary --> FlagMaster
    Binary --> FlagNode

    FlagMaster --> CompMaster["REST API + Scheduler\n+ Registry + Coordinator\n+ Storage"]
    FlagNode --> CompNode["Worker Server\n+ Heartbeat Loop"]
```

Desain satu binary mempermudah distribusi: cukup copy `distapi.exe` ke semua laptop. Tidak ada dependency runtime tambahan.

## 4. Komponen Master

```mermaid
flowchart TD
    ConfigParse["config.Parse()\nflag → env → default"]

    subgraph Components["Komponen Master"]
        Storage["storage.Store\nAtomic file I/O"]
        Registry["registry.Registry\nHome-based naming"]
        NodeClient["rpc.NodeClientAdapter\nCached gRPC connections"]
        Scheduler["scheduler.Scheduler\nScatter-gather dispatch"]
        CoordServer["rpc.CoordinatorServer\nRegister + Heartbeat RPC"]
        WorkerServer["rpc.WorkerServer\nMaster-local fallback"]
        APIHandler["api.Handler\n7 REST endpoints"]
    end

    Goroutines["Background Goroutines\nDeadNodeMonitor (1s tick)\nJobTTLCleaner (5m tick)"]

    ConfigParse --> Storage
    ConfigParse --> Registry
    Storage --> Scheduler
    Registry --> Scheduler
    Registry --> CoordServer
    NodeClient --> Scheduler
    Scheduler --> APIHandler
    Registry --> APIHandler
    Storage --> APIHandler
    CoordServer --> Registry
    Scheduler --> Goroutines
    Registry --> Goroutines
```

## 5. Alur Data End-to-End

```mermaid
sequenceDiagram
    participant C as Klien
    participant API as REST API (Master)
    participant Sched as Scheduler
    participant Store as Storage
    participant Node as Node (gRPC Worker)

    C->>API: POST /api/v1/jobs (multipart gambar)
    API->>API: Validasi format + ukuran
    API->>Sched: Submit(filenames, imageData, opts)
    Sched->>Store: SaveUpload setiap gambar
    Sched-->>API: return jobID
    API-->>C: 202 Accepted + {job_id}

    Note over Sched,Node: Async — goroutine dispatchJob

    par Scatter ke setiap gambar
        Sched->>Node: ProcessImage(taskID, imageData, opts)
        Node->>Node: worker.Process() — resize + grayscale
        Node-->>Sched: return resultData
        Sched->>Store: SaveResult (atomic write)
    end

    Sched->>Sched: Agregasi status → JobDone

    loop Polling klien
        C->>API: GET /api/v1/jobs/{id}
        API-->>C: {status: "PROCESSING", done: 2, total: 3}
    end

    C->>API: GET /api/v1/jobs/{id}
    API-->>C: {status: "DONE"}
    C->>API: GET /api/v1/jobs/{id}/results/foto.jpg
    API-->>C: stream file hasil
```

## 6. Dependency Antar Package

```mermaid
flowchart LR
    Main["cmd/distapi\nmain.go"]
    Config["internal/config"]
    API["internal/api"]
    Sched["internal/scheduler"]
    Reg["internal/registry"]
    RPC["internal/rpc"]
    Store["internal/storage"]
    Worker["internal/worker"]
    GenCluster["gen/cluster\ngRPC stubs"]

    Main --> Config
    Main --> API
    Main --> Sched
    Main --> Reg
    Main --> RPC
    Main --> Store
    Main --> GenCluster

    API --> Sched
    API --> Reg
    API --> Store

    Sched --> Reg
    Sched --> Store

    RPC --> Reg
    RPC --> Worker
    RPC --> GenCluster
```

> Tidak ada circular dependency. `scheduler` tidak mengimpor `rpc` — interface `NodeClient` digunakan untuk decoupling.

## 7. Port dan Protokol

| Port | Protokol | Dipakai Oleh | Arah |
|------|----------|-------------|------|
| 8080 | HTTP/1.1 | REST API | Klien → Master |
| 9000 | HTTP/2 (gRPC) | Coordinator | Node → Master |
| 9000 | HTTP/2 (gRPC) | Worker | Master → Node |

> Semua node menggunakan port 9000 yang sama karena setiap laptop punya IP berbeda. Master tahu port node dari nilai `--advertise`.
