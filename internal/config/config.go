// Package config menangani konfigurasi runtime aplikasi distapi.
//
// Prioritas pembacaan nilai: flag CLI > variabel lingkungan > nilai default.
// Mengikuti prinsip 12-Factor App sehingga tidak perlu menyalin file konfigurasi
// ke setiap laptop — cukup set flag atau env saat binary dijalankan.
//
// Semua parameter divalidasi sekali saat startup; konfigurasi salah langsung
// menyebabkan program berhenti dengan pesan jelas (fail-fast).
package config

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Mode menentukan apakah binary berjalan sebagai master atau node.
type Mode string

const (
	// ModeMaster menjalankan REST API, scheduler, registry, dan worker lokal.
	ModeMaster Mode = "master"
	// ModeNode menjalankan gRPC Worker server dan mengirim heartbeat ke master.
	ModeNode Mode = "node"
)

// Config berisi seluruh konfigurasi runtime. Semua field sudah tervalidasi.
type Config struct {
	Mode     Mode
	LogLevel slog.Level

	// Port HTTP hanya aktif di master untuk melayani REST API.
	HTTPPort int

	// Port gRPC dipakai oleh master (Coordinator) dan node (Worker).
	GRPCPort int

	// Token adalah shared secret yang memvalidasi setiap RPC antar node.
	Token string

	// Workers adalah jumlah goroutine pemroses gambar per binary.
	// Master dibatasi setengah CPU agar tetap responsif untuk heartbeat dan REST.
	Workers int

	// DataDir menyimpan file upload dan hasil sementara di master.
	DataDir string

	// JobTTL menentukan berapa lama job disimpan setelah selesai.
	JobTTL time.Duration

	// MaxImageMB dan MaxImages membatasi ukuran dan jumlah gambar per job.
	MaxImageMB int
	MaxImages  int

	// NodeID adalah nama unik node dalam kluster (default: hostname).
	NodeID string

	// MasterAddr adalah alamat gRPC master yang digunakan node untuk Register/Heartbeat.
	MasterAddr string

	// AdvertiseAddr adalah IP:port node yang bisa dijangkau oleh master.
	// Jika kosong, akan di-auto-detect dari antarmuka jaringan aktif.
	AdvertiseAddr string

	// HeartbeatInterval menentukan seberapa sering node mengirim heartbeat.
	HeartbeatInterval time.Duration

	// NodeTimeout: master menganggap node mati jika tidak ada heartbeat selama durasi ini.
	// Disarankan 3× HeartbeatInterval agar toleran terhadap delay jaringan sesaat.
	NodeTimeout time.Duration

	// TaskTimeout: batas waktu setiap panggilan ProcessImage ke node.
	// Nilai awal 30 detik; ukur di jaringan nyata sebelum demo.
	TaskTimeout time.Duration

	// MaxRetries: berapa kali task boleh dicoba ulang sebelum ditandai FAILED.
	// Nilai 3 dipilih berdasarkan DESIGN.md §7.4.
	MaxRetries int
}

// Parse membaca os.Args dan mengembalikan Config yang sudah tervalidasi.
// Fungsi ini memanggil os.Exit(1) jika ada parameter wajib yang hilang atau salah.
func Parse() Config {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	// Daftarkan semua flag; nilai default dibaca dari env jika tersedia.
	mode       := fs.String("mode",                envOr("MODE", ""),                              "master|node (wajib)")
	httpPort   := fs.Int("http-port",               envOrInt("HTTP_PORT", 8080),                   "port HTTP (master)")
	grpcPort   := fs.Int("grpc-port",               envOrInt("GRPC_PORT", 9000),                   "port gRPC (keduanya)")
	nodeID     := fs.String("node-id",              envOr("NODE_ID", hostname()),                  "identitas node")
	masterAddr := fs.String("master",               envOr("MASTER_ADDR", ""),                      "alamat master IP:port (wajib di mode node)")
	advertise  := fs.String("advertise",            envOr("ADVERTISE_ADDR", ""),                   "IP:port node yang bisa dijangkau master")
	token      := fs.String("token",                envOr("TOKEN", ""),                             "shared cluster secret (wajib)")
	workers    := fs.Int("workers",                 envOrInt("WORKERS", 0),                        "goroutine pemroses (0 = otomatis)")
	dataDir    := fs.String("data-dir",             envOr("DATA_DIR", "./data"),                   "direktori data sementara (master)")
	maxImgMB   := fs.Int("max-image-mb",            envOrInt("MAX_IMAGE_MB", 5),                   "batas ukuran gambar dalam MB")
	maxImages  := fs.Int("max-images",              envOrInt("MAX_IMAGES", 20),                    "batas jumlah gambar per job")
	jobTTLStr  := fs.String("job-ttl",              envOr("JOB_TTL", "1h"),                        "retensi job setelah selesai")
	hbInterval := fs.Duration("heartbeat-interval", envOrDuration("HEARTBEAT_INTERVAL", 2*time.Second), "interval heartbeat node")
	nodeTmt    := fs.Duration("node-timeout",       envOrDuration("NODE_TIMEOUT", 6*time.Second),  "batas waktu tanpa heartbeat sebelum node dianggap mati")
	taskTmt    := fs.Duration("task-timeout",       envOrDuration("TASK_TIMEOUT", 30*time.Second), "batas waktu per task ProcessImage")
	maxRetries := fs.Int("max-retries",             envOrInt("MAX_RETRIES", 3),                    "maks percobaan ulang per task")
	logLevel   := fs.String("log-level",            envOr("LOG_LEVEL", "info"),                   "level log: debug|info|warn|error")

	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "config: gagal parse argumen:", err)
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
		errs = append(errs, "--mode harus 'master' atau 'node'")
	}

	if *token == "" {
		errs = append(errs, "--token wajib diisi (shared cluster secret)")
	}
	if m == ModeNode && *masterAddr == "" {
		errs = append(errs, "--master wajib diisi di mode node")
	}

	jobTTL, err := time.ParseDuration(*jobTTLStr)
	if err != nil {
		errs = append(errs, fmt.Sprintf("--job-ttl %q bukan durasi yang valid: %v", *jobTTLStr, err))
		jobTTL = time.Hour
	}

	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "config error:", e)
		}
		os.Exit(1)
	}

	w := *workers
	if w <= 0 {
		if m == ModeMaster {
			// Master juga melayani REST dan heartbeat; batasi setengah CPU
			// agar node sehat tidak salah ditandai mati karena CPU penuh.
			w = maxInt(1, runtime.NumCPU()/2)
		} else {
			w = runtime.NumCPU()
		}
	}

	level := parseLogLevel(*logLevel)

	// Auto-detect alamat yang bisa dijangkau jika belum diset di mode node.
	adv := *advertise
	if adv == "" && m == ModeNode {
		adv = autoDetectIP(*grpcPort)
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
	}
}

// Validate mengembalikan error jika konfigurasi jelas tidak valid.
// Dipakai dalam tes unit tanpa harus memanggil Parse().
func (c Config) Validate() error {
	if c.Token == "" {
		return errors.New("config: token kosong")
	}
	return nil
}

// envOr mengembalikan nilai env DISTAPI_<KEY> atau def jika tidak ada.
func envOr(key, def string) string {
	if v := os.Getenv("DISTAPI_" + key); v != "" {
		return v
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

// autoDetectIP mencari IP non-loopback pertama dan menambahkan port.
// Bermanfaat saat --advertise tidak diset eksplisit di WiFi yang IP-nya dari DHCP.
func autoDetectIP(port int) string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return fmt.Sprintf("127.0.0.1:%d", port)
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return fmt.Sprintf("%s:%d", ipnet.IP.String(), port)
		}
	}
	return fmt.Sprintf("127.0.0.1:%d", port)
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
