// Package config mem-parsing dan memvalidasi konfigurasi runtime aplikasi.
package config

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"distapi/internal/rmi"
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
	RMIPort           int
	Priority          int
	Peers             string
	AutoFailover      bool
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

// PrintTeam mencetak daftar nama pengembang tanpa rincian pembagian tugas.
func PrintTeam(w io.Writer) {
	fmt.Fprint(w, `distapi — Distributed Image Processing System
Sistem Terdistribusi (IF2228) · Teknik Informatika · Universitas Trunojoyo Madura

TIM PENGEMBANG:
  No  NIM           Nama
  1   240411100001  Rafli Khiyanuran Bazhari
  2   240411100014  Irma Annisatul Jannah
  3   240411100103  Muhammad Fajar Nugroho
  4   240411100144  Zakaria Mujur Prasetyo
`)
}

// PrintVersion mencetak nomor versi aplikasi.
func PrintVersion(w io.Writer) {
	fmt.Fprintln(w, "distapi v0.1.0")
}

// printQuickExamples mencetak contoh penggunaan singkat saat biner dijalankan tanpa argumen.
func printQuickExamples(w io.Writer) {
	fmt.Fprint(w, `distapi — Distributed Image Processing System (Sistem Terdistribusi IF2228)

CONTOH PENGGUNAAN CEPAT:

  1. Jalankan Laptop 1 sebagai Master (Visual TUI):
     .\dist\distapi.exe --mode=master --token=demo123 --tui

  2. Jalankan Laptop 2, 3, 4 sebagai Node Worker:
     .\dist\distapi.exe --mode=node --master=<IP_MASTER>:9000 --token=demo123 --tui

BANTUAN & INFORMASI:
  .\distapi.exe -h       Tampilkan parameter inti dan opsi umum
  .\distapi.exe --help   Tampilkan seluruh parameter teknis, REST API, & troubleshooting
  .\distapi.exe team     Tampilkan daftar nama tim pengembang
`)
}

// printCoreHelp mencetak parameter inti dan sintaks dasar (-h).
func printCoreHelp(w io.Writer) {
	fmt.Fprint(w, `distapi — Distributed Image Processing System (Sistem Terdistribusi IF2228)

SINTAKS:
  distapi --mode=master [opsi...]
  distapi --mode=node --master=<host:port> [opsi...]
  distapi rmi-test [host:port]
  distapi team | authors
  distapi -h | --help | --version

PARAMETER INTI:
  --mode string
        Mode operasional kluster: "master" atau "node" [WAJIB]
  --token string
        Shared secret token autentikasi gRPC antar-node kluster (default "demo123")
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
  --rmi-port int
        Port komunikasi Remote Method Invocation (RMI) net/rpc (default 9050)
  --priority int
        Prioritas pemilihan koordinator / Bully Algorithm (default 0: otomatis sesuai ID)
  --peers string
        Daftar alamat RMI node peer (format host:port dipisahkan koma)
  --auto-failover
        Aktifkan deteksi & pemilihan koordinator otomatis jika master mati (default true)
  --log-level string
        Level pencatatan teks: debug | info | warn | error (default "info")

PERINTAH INFORMASI & PENGUJIAN:
  distapi team             Tampilkan daftar nama tim pengembang (alias: authors, about)
  distapi rmi-test [addr]  Uji koneksi dan pemanggilan remote method invocation (default: 127.0.0.1:9050)
  distapi --version        Tampilkan versi aplikasi (alias: -v, version)

CONTOH OPERASIONAL (PowerShell):
  * Master Visual TUI : .\dist\distapi.exe --mode=master --tui
  * Worker Visual TUI : .\dist\distapi.exe --mode=node --master=192.168.1.10:9000 --tui
  * Master Mode CLI   : .\dist\distapi.exe --mode=master
  * Worker Mode CLI   : .\dist\distapi.exe --mode=node --master=192.168.1.10:9000
  * Uji Layanan RMI   : .\dist\distapi.exe rmi-test 127.0.0.1:9050

DOKUMENTASI LENGKAP:
  Jalankan 'distapi --help' untuk opsi sinkronisasi, detektor kegagalan, RMI, & troubleshooting.
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
  distapi team | authors | version
  distapi -h | --help | help

SUB-PERINTAH & INFORMASI:
  team, authors, about
        Tampilkan daftar nama anggota tim pengembang
  rmi-test [target]
        Uji koneksi dan remote method invocation ke node target (default: 127.0.0.1:9050)
  version, --version, -v
        Tampilkan nomor versi rilis aplikasi

PARAMETER UTAMA:
  --mode string
        Mode operasional node: "master" atau "node"
        [WAJIB]
  --token string
        Shared secret token untuk autentikasi gRPC antar-node kluster (default "demo123")
  --master string
        Alamat gRPC master tujuan (format host:port, contoh: 192.168.1.10:9000)
        [WAJIB pada mode node]

OPSI SINKRONISASI & REMOTE METHOD INVOCATION (RMI):
  --rmi-port int
        Port komunikasi Remote Method Invocation (RMI) net/rpc (default 9050)
  --priority int
        Nilai prioritas pemilihan koordinator pada Algoritma Bully
        (default 0: otomatis diekstrak dari angka ID node)
  --peers string
        Daftar alamat endpoint RMI peer node lain (dipisahkan koma, contoh: 192.168.1.11:9050,192.168.1.12:9050)
  --auto-failover
        Aktifkan deteksi kegagalan koordinator dan inisiasi pemilihan koordinator baru
        secara otomatis saat master tidak merespons (default true)

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
    curl.exe -X POST http://localhost:8080/api/v1/jobs -F "images=@foto1.jpg" -F "images=@foto2.png" -F "options={\"resize_width\":800,\"resize_height\":800,\"grayscale\":true}"

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

	// Deteksi sub-perintah posisi pertama (subcommand mandiri, misal: distapi team)
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		switch strings.ToLower(os.Args[1]) {
		case "team", "authors", "about", "credits":
			PrintTeam(os.Stdout)
			os.Exit(0)
		case "version":
			PrintVersion(os.Stdout)
			os.Exit(0)
		case "rmi-test", "test-rmi":
			target := "127.0.0.1:9050"
			if len(os.Args) > 2 {
				target = os.Args[2]
			}
			RunRMITest(target)
			os.Exit(0)
		case "help":
			printFullHelp(os.Stdout)
			os.Exit(0)
		}
	}

	// Deteksi eksplisit flag bantuan sebelum parsing flag
	for _, arg := range os.Args[1:] {
		low := strings.ToLower(arg)
		if low == "--help" {
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

	showTeam := fs.Bool("team", false, "tampilkan daftar nama tim pengembang")
	fs.BoolVar(showTeam, "authors", false, "alias untuk --team")
	fs.BoolVar(showTeam, "about", false, "alias untuk --team")
	showVersion := fs.Bool("version", false, "tampilkan versi aplikasi")
	fs.BoolVar(showVersion, "v", false, "alias untuk --version")

	mode := fs.String("mode", envOr("MODE", ""), "mode operasional: master|node")
	httpPort := fs.Int("http-port", envOrInt("HTTP_PORT", 8080), "port HTTP server (master)")
	grpcPort := fs.Int("grpc-port", envOrInt("GRPC_PORT", 9000), "port gRPC server")
	rmiPort := fs.Int("rmi-port", envOrInt("RMI_PORT", 9050), "port Remote Method Invocation (RMI) net/rpc server")
	priority := fs.Int("priority", envOrInt("PRIORITY", 0), "prioritas pemilihan koordinator (0 = otomatis sesuai ID node)")
	peers := fs.String("peers", envOr("PEERS", ""), "daftar alamat RMI peer node lain (dipisahkan koma)")
	autoFailover := fs.Bool("auto-failover", envOrBool("AUTO_FAILOVER", true), "aktifkan deteksi dan pemilihan koordinator otomatis jika koordinator mati")
	nodeID := fs.String("node-id", envOr("NODE_ID", "auto"), "identitas unik node (default 'auto': dialokasikan otomatis oleh master)")
	masterAddr := fs.String("master", envOrFallback("MASTER", "MASTER_ADDR", ""), "alamat gRPC master (host:port)")
	advertise := fs.String("advertise", envOrFallback("ADVERTISE", "ADVERTISE_ADDR", ""), "alamat gRPC node yang dapat dijangkau master")
	token := fs.String("token", envOr("TOKEN", "demo123"), "shared secret autentikasi cluster (default 'demo123')")
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

	if *showTeam {
		PrintTeam(os.Stdout)
		os.Exit(0)
	}
	if *showVersion {
		PrintVersion(os.Stdout)
		os.Exit(0)
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

	if strings.TrimSpace(*token) == "" {
		errs = append(errs, "flag --token tidak boleh kosong")
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
		RMIPort:           *rmiPort,
		Priority:          *priority,
		Peers:             *peers,
		AutoFailover:      *autoFailover,
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

func envOrFallback(primary, secondary, def string) string {
	if v := os.Getenv("DISTAPI_" + primary); v != "" {
		return v
	}
	if v := os.Getenv("DISTAPI_" + secondary); v != "" {
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

// RunRMITest menguji fungsionalitas remote method invocation (RMI) pada node target.
func RunRMITest(target string) {
	fmt.Printf("Menguji koneksi Remote Method Invocation (RMI) ke %s...\n\n", target)

	// 1. Uji Ping RMI
	start := time.Now()
	pingReply, err := rmi.Ping(target, "distapi-tester", 3*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[GAGAL] Tidak dapat menghubungi RMI server di %s: %v\n", target, err)
		fmt.Fprintf(os.Stderr, "Pastikan distapi berjalan dan port RMI terbuka (default 9050).\n")
		os.Exit(1)
	}
	rtt := time.Since(start)
	serverTime := time.UnixMilli(pingReply.Timestamp)
	fmt.Printf("[OK] RMI Ping Berhasil!\n")
	fmt.Printf("     Node ID       : %s\n", pingReply.NodeID)
	fmt.Printf("     Peran Node    : %s\n", pingReply.Role)
	fmt.Printf("     Status        : %s\n", pingReply.Status)
	fmt.Printf("     Task Aktif    : %d\n", pingReply.ActiveTasks)
	fmt.Printf("     Waktu Server  : %s (RTT: %v)\n\n", serverTime.Format(time.RFC3339), rtt)

	// 2. Buat citra uji coba 16x16 PNG sintetis
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 15), G: uint8(y * 15), B: 180, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Gagal membuat citra pengujian: %v\n", err)
		return
	}

	// 3. Uji Remote Method Invocation untuk Transformasi Citra
	fmt.Printf("Menguji pemanggilan metode remote ImageProcessor.TransformImage...\n")
	t0 := time.Now()
	procReply, err := rmi.InvokeProcessImage(target, rmi.ProcessImageArgs{
		TaskID:       "test-task-rmi-001",
		JobID:        "job-rmi-test",
		Filename:     "test_sample.png",
		ImageData:    buf.Bytes(),
		ResizeWidth:  8,
		ResizeHeight: 8,
		Grayscale:    true,
	}, 5*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[GAGAL] Pemanggilan RMI ImageProcessor.TransformImage gagal: %v\n", err)
		os.Exit(1)
	}
	dur := time.Since(t0)

	fmt.Printf("[OK] Transformasi Citra RMI Berhasil!\n")
	fmt.Printf("     Task ID       : %s\n", procReply.TaskID)
	fmt.Printf("     Status Sukses : %t\n", procReply.Success)
	fmt.Printf("     Ukuran Awal   : %d bytes\n", len(buf.Bytes()))
	fmt.Printf("     Ukuran Hasil  : %d bytes\n", len(procReply.ResultData))
	fmt.Printf("     Durasi Komputasi : %d ms (Total RTT: %v)\n\n", procReply.DurationMs, dur)
	fmt.Printf("Pengujian RMI selesai dengan sukses. Protokol sinkronisasi & komputasi terdistribusi aktif.\n")
}
