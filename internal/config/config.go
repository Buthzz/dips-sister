// Package config mem-parsing dan memvalidasi konfigurasi runtime aplikasi.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Mode operasi binary: master atau node.
type Mode string

const (
	ModeMaster Mode = "master"
	ModeNode   Mode = "node"
)

// Config memuat parameter operasional node dan cluster.
type Config struct {
	Mode              Mode
	LogLevel          slog.Level
	HTTPPort          int
	GRPCPort          int
	Token             string
	Workers           int
	DataDir           string
	JobTTL            time.Duration
	MaxImageMB        int
	MaxImages         int
	NodeID            string
	MasterAddr        string
	AdvertiseAddr     string
	HeartbeatInterval time.Duration
	NodeTimeout       time.Duration
	TaskTimeout       time.Duration
	MaxRetries        int
	TUI               bool
}

// printQuickExamples mencetak contoh penggunaan singkat saat biner dijalankan tanpa argumen.
func printQuickExamples(w io.Writer) {
	fmt.Fprint(w, `distapi — Distributed Image Processing System (Sistem Terdistribusi IF2228)

CONTOH PENGGUNAAN CEPAT:

  1. Jalankan Laptop 1 sebagai Master (Visual TUI):
     .\dist\distapi.exe --mode=master --token=demo123 --tui

  2. Jalankan Laptop 2, 3, 4 sebagai Node Worker:
     .\dist\distapi.exe --mode=node --master=<IP_MASTER>:9000 --token=demo123 --tui

BANTUAN & PANDUAN:
  .\distapi.exe -h       Tampilkan parameter inti dan opsi umum
  .\distapi.exe --help   Tampilkan seluruh parameter teknis, REST API, & troubleshooting
`)
}

// printCoreHelp mencetak parameter inti dan sintaks dasar (-h).
func printCoreHelp(w io.Writer) {
	fmt.Fprint(w, `distapi — Distributed Image Processing System (Sistem Terdistribusi IF2228)

SINTAKS:
  distapi --mode=master --token=<token> [opsi...]
  distapi --mode=node --node-id=<id> --master=<host:port> --token=<token> [opsi...]
  distapi -h | --help

PARAMETER INTI:
  --mode string
        Mode operasional kluster: "master" atau "node" [WAJIB]
  --token string
        Shared secret token autentikasi gRPC antar-node kluster [WAJIB]
  --master string
        Alamat gRPC master tujuan (host:port, misal: 192.168.1.10:9000) [WAJIB pada mode node]
  --node-id string
        Identitas unik node worker (default "auto": dialokasikan otomatis oleh master)
  --tui
        Aktifkan antarmuka visual terminal interaktif berbasis Bubble Tea
  --advertise string
        Alamat gRPC node yang dapat dihubungi master (default: auto-detect IP fisik)
  --workers int
        Jumlah goroutine pemroses paralel (default: otomatis sesuai jumlah core CPU)
  --http-port int
        Port antarmuka REST API gateway dan Web UI pada master (default 8080)
  --grpc-port int
        Port komunikasi protokol biner gRPC (default 9000)
  --log-level string
        Level pencatatan teks: debug | info | warn | error (default "info")

CONTOH OPERASIONAL (PowerShell):
  * Master Visual TUI : .\dist\distapi.exe --mode=master --token=demo123 --tui
  * Worker Visual TUI : .\dist\distapi.exe --mode=node --master=192.168.1.10:9000 --token=demo123 --tui
  * Master Mode CLI   : .\dist\distapi.exe --mode=master --token=demo123
  * Worker Mode CLI   : .\dist\distapi.exe --mode=node --master=192.168.1.10:9000 --token=demo123

DOKUMENTASI LENGKAP:
  Jalankan 'distapi --help' untuk opsi detektor kegagalan, retensi, pengujian curl, & mitigasi firewall.
`)
}

// printFullHelp mencetak seluruh opsi parameter, arsitektur, pengujian REST API, dan troubleshooting (--help).
func printFullHelp(w io.Writer) {
	fmt.Fprint(w, `distapi — Distributed Image Processing System (Sistem Terdistribusi IF2228)

DESKRIPSI:
  distapi adalah sistem pemrosesan citra digital terdistribusi berbasis arsitektur
  Master-Slave yang dikompilasi menjadi satu biner mandiri (single binary).
  Master mengorkestrasi komputasi dengan pola Scatter-Gather dan Round-Robin dispatch,
  sementara Node worker memproses transformasi citra (aspect resize & grayscale) secara paralel.

SINTAKS:
  distapi --mode=master [opsi...]
  distapi --mode=node --master=<host:port> [opsi...]
  distapi -h | --help | help

PARAMETER UTAMA:
  --mode string
        Mode operasional node: "master" atau "node"
        [WAJIB]
  --token string
        Shared secret token untuk autentikasi gRPC antar-node kluster
        [WAJIB di kedua mode]
  --master string
        Alamat gRPC master tujuan (format host:port, contoh: 192.168.1.10:9000)
        [WAJIB pada mode node]

OPSI TAMPILAN & LOGGING:
  --tui
        Aktifkan antarmuka visual terminal interaktif berbasis Bubble Tea
        (output log sistem otomatis dialihkan ke berkas distapi-tui.log)
  --log-level string
        Level pencatatan teks: debug | info | warn | error (default "info")

OPSI JARINGAN & IDENTITAS:
  --node-id string
        Identitas unik node worker dalam kluster (default "auto": dialokasikan otomatis oleh master)
  --advertise string
        Alamat gRPC node yang dapat dihubungi balik oleh master (format host:port)
        (default: otomatis mendeteksi IP antarmuka Wi-Fi/LAN fisik yang aktif)
  --http-port int
        Port antarmuka REST API gateway dan Web UI pada master (default 8080)
  --grpc-port int
        Port komunikasi protokol biner gRPC (default 9000)

OPSI RESOURCE & PENJADWALAN KOMPUTASI:
  --workers int
        Jumlah goroutine pemroses paralel (default: otomatis sesuai jumlah core CPU)
  --data-dir string
        Direktori penyimpanan berkas gambar sementara (default "./data")
  --max-image-mb int
        Batas ukuran maksimum satu berkas gambar dalam MB (default 5)
  --max-images int
        Batas jumlah gambar maksimum per job (default 20)
  --max-retries int
        Batas toleransi percobaan ulang task jika node mengalami kegagalan (default 3)

OPSI DETEKTOR KEGAGALAN (FAILURE DETECTOR):
  --heartbeat-interval duration
        Interval waktu pengiriman sinyal detak jantung ke master (default 2s)
  --node-timeout duration
        Batas waktu sebelum master menyatakan node worker mati/dead (default 6s)
  --task-timeout duration
        Batas waktu pemrosesan satu task sebelum dialihkan ke node lain (default 30s)
  --job-ttl duration
        Masa simpan job yang selesai sebelum dibersihkan dari memori (default 1h)

CONTOH OPERASIONAL KLUSTER (Windows PowerShell):

  1. Jalankan Laptop 1 sebagai Master (CLI biasa):
     .\dist\distapi.exe --mode=master --token=demo123

  2. Jalankan Laptop 1 sebagai Master dengan Visual TUI:
     .\dist\distapi.exe --mode=master --token=demo123 --tui

  3. Jalankan Laptop 2, 3, 4 sebagai Node Worker (IP & ID otomatis):
     .\dist\distapi.exe --mode=node --master=192.168.1.10:9000 --token=demo123 --tui

  4. Jalankan Node Worker dengan ID manual atau alamat advertise manual:
     .\dist\distapi.exe --mode=node --node-id=node-custom --master=192.168.1.10:9000 --advertise=192.168.1.12:9000 --token=demo123 --tui

CONTOH PENGUJIAN REST API (curl.exe / PowerShell):

  * Cek Kesehatan Master:
    curl.exe http://localhost:8080/healthz

  * Cek Daftar Node Aktif:
    curl.exe http://localhost:8080/api/v1/nodes

  * Uji Konektivitas Node (TCP Probe):
    curl.exe http://localhost:8080/api/v1/nodes/node-1/probe

  * Kirim Job Pemrosesan Gambar (Scatter-Gather):
    curl.exe -X POST http://localhost:8080/api/v1/jobs -F "images=@foto1.jpg" -F "images=@foto2.png" -F "resize_w=400" -F "grayscale=true"

CATATAN & TROUBLESHOOTING:
  - Setiap opsi CLI dapat dikonfigurasi melalui environment variable dengan prefiks
    DISTAPI_ (contoh: DISTAPI_TOKEN=demo123, DISTAPI_MODE=master, DISTAPI_TUI=true).
  - Pastikan port 9000 TCP (gRPC) dan 8080 TCP (HTTP) diizinkan pada Windows Firewall
    di seluruh laptop agar koneksi antar-node tidak terblokir.
  - Jika menggunakan Wi-Fi publik/kampus yang menerapkan Client Isolation, gunakan
    Hotspot Portabel dari HP agar seluruh laptop dapat saling berkomunikasi secara langsung.
`)
}

// Parse membaca konfigurasi dari CLI flag dan environment variables.
func Parse() Config {
	// Jika dijalankan tanpa argumen: tampilkan contoh ringkas
	if len(os.Args) == 1 {
		printQuickExamples(os.Stdout)
		os.Exit(0)
	}

	// Deteksi flag bantuan sebelum parsing flag
	for _, arg := range os.Args[1:] {
		low := strings.ToLower(arg)
		if low == "--help" || low == "help" {
			printFullHelp(os.Stdout)
			os.Exit(0)
		}
		if low == "-h" || low == "-help" {
			printCoreHelp(os.Stdout)
			os.Exit(0)
		}
	}

	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	fs.Usage = func() {
		printCoreHelp(fs.Output())
	}

	mode := fs.String("mode", envOr("MODE", ""), "mode operasional: master|node")
	httpPort := fs.Int("http-port", envOrInt("HTTP_PORT", 8080), "port HTTP server (master)")
	grpcPort := fs.Int("grpc-port", envOrInt("GRPC_PORT", 9000), "port gRPC server")
	nodeID := fs.String("node-id", envOr("NODE_ID", "auto"), "identitas unik node (default 'auto': dialokasikan otomatis oleh master)")
	masterAddr := fs.String("master", envOr("MASTER_ADDR", ""), "alamat gRPC master (host:port)")
	advertise := fs.String("advertise", envOr("ADVERTISE_ADDR", ""), "alamat gRPC node yang dapat dijangkau master")
	token := fs.String("token", envOr("TOKEN", ""), "shared secret autentikasi cluster")
	workers := fs.Int("workers", envOrInt("WORKERS", 0), "jumlah worker goroutine (0 = otomatis)")
	dataDir := fs.String("data-dir", envOr("DATA_DIR", "./data"), "direktori penyimpanan berkas sementara")
	maxImgMB := fs.Int("max-image-mb", envOrInt("MAX_IMAGE_MB", 5), "ukuran maksimum satu berkas gambar (MB)")
	maxImages := fs.Int("max-images", envOrInt("MAX_IMAGES", 20), "jumlah maksimum gambar per job")
	jobTTLStr := fs.String("job-ttl", envOr("JOB_TTL", "1h"), "retensi job sebelum dihapus otomatis")
	hbInterval := fs.Duration("heartbeat-interval", envOrDuration("HEARTBEAT_INTERVAL", 2*time.Second), "interval pengiriman heartbeat")
	nodeTmt := fs.Duration("node-timeout", envOrDuration("NODE_TIMEOUT", 6*time.Second), "batas waktu deteksi node mati")
	taskTmt := fs.Duration("task-timeout", envOrDuration("TASK_TIMEOUT", 30*time.Second), "batas waktu pemrosesan satu task")
	maxRetries := fs.Int("max-retries", envOrInt("MAX_RETRIES", 3), "maksimum percobaan ulang task yang gagal")
	logLevel := fs.String("log-level", envOr("LOG_LEVEL", "info"), "level logging: debug|info|warn|error")
	tui := fs.Bool("tui", envOrBool("TUI", false), "aktifkan antarmuka terminal interaktif (Bubble Tea)")

	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "config: gagal memproses argumen:", err)
		os.Exit(1)
	}

	var errs []string

	var m Mode
	switch strings.ToLower(*mode) {
	case "master":
		m = ModeMaster
	case "node":
		m = ModeNode
	default:
		errs = append(errs, "flag --mode harus bernilai 'master' atau 'node'")
	}

	if *token == "" {
		errs = append(errs, "flag --token wajib diisi")
	}
	if m == ModeNode && *masterAddr == "" {
		errs = append(errs, "flag --master wajib diisi saat mode node")
	}

	jobTTL, err := time.ParseDuration(*jobTTLStr)
	if err != nil {
		errs = append(errs, fmt.Sprintf("flag --job-ttl tidak valid: %v", err))
		jobTTL = time.Hour
	}

	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "config error:", e)
		}
		fmt.Fprintln(os.Stderr, "\nJalankan 'distapi -h' untuk opsi inti atau 'distapi --help' untuk panduan lengkap.")
		os.Exit(1)
	}

	w := *workers
	if w <= 0 {
		if m == ModeMaster {
			// Batasi worker master agar tidak memonopoli CPU dari tugas koordinasi
			w = maxInt(1, runtime.NumCPU()/2)
		} else {
			w = runtime.NumCPU()
		}
	}

	level := parseLogLevel(*logLevel)

	adv := *advertise
	if adv == "" && m == ModeNode {
		adv = autoDetectIP(*grpcPort)
		fmt.Fprintf(os.Stderr, "config: advertise addr auto-detect: %s\n", adv)
	}

	// B1: validasi format host:port setelah nilai adv tersedia
	if adv != "" {
		if _, _, err := net.SplitHostPort(adv); err != nil {
			errs = append(errs, fmt.Sprintf(
				"flag --advertise '%s' tidak valid (harus format host:port, contoh: 192.168.1.11:9000): %v",
				adv, err))
		}
	}

	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "config error:", e)
		}
		os.Exit(1)
	}

	return Config{
		Mode:              m,
		LogLevel:          level,
		HTTPPort:          *httpPort,
		GRPCPort:          *grpcPort,
		NodeID:            *nodeID,
		MasterAddr:        *masterAddr,
		AdvertiseAddr:     adv,
		Token:             *token,
		Workers:           w,
		DataDir:           *dataDir,
		MaxImageMB:        *maxImgMB,
		MaxImages:         *maxImages,
		JobTTL:            jobTTL,
		HeartbeatInterval: *hbInterval,
		NodeTimeout:       *nodeTmt,
		TaskTimeout:       *taskTmt,
		MaxRetries:        *maxRetries,
		TUI:               *tui,
	}
}

// Validate memeriksa integritas dasar konfigurasi.
func (c Config) Validate() error {
	if c.Token == "" {
		return errors.New("config: token tidak boleh kosong")
	}
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv("DISTAPI_" + key); v != "" {
		return v
	}
	return def
}

func envOrBool(key string, def bool) bool {
	if v := os.Getenv("DISTAPI_" + key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func envOrInt(key string, def int) int {
	if v := os.Getenv("DISTAPI_" + key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envOrDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv("DISTAPI_" + key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func hostname() string {
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "unknown"
}

// DetectLocalIP mendeteksi alamat IPv4 fisik antarmuka Wi-Fi atau LAN yang aktif,
// mengabaikan interface loopback, interface mati, dan adapter virtual (VMware, VirtualBox, WSL, Hyper-V).
func DetectLocalIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		nameLower := strings.ToLower(iface.Name)
		if strings.Contains(nameLower, "vmware") ||
			strings.Contains(nameLower, "virtualbox") ||
			strings.Contains(nameLower, "vethernet") ||
			strings.Contains(nameLower, "hyper-v") ||
			strings.Contains(nameLower, "wsl") ||
			strings.Contains(nameLower, "loopback") {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
				continue
			}
			return ip4.String()
		}
	}
	return "127.0.0.1"
}

func autoDetectIP(port int) string {
	return fmt.Sprintf("%s:%d", DetectLocalIP(), port)
}


func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
