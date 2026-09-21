// Package config mem-parsing dan memvalidasi konfigurasi runtime aplikasi.
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
}

// Parse membaca konfigurasi dari CLI flag dan environment variables.
func Parse() Config {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	mode := fs.String("mode", envOr("MODE", ""), "mode operasional: master|node")
	httpPort := fs.Int("http-port", envOrInt("HTTP_PORT", 8080), "port HTTP server (master)")
	grpcPort := fs.Int("grpc-port", envOrInt("GRPC_PORT", 9000), "port gRPC server")
	nodeID := fs.String("node-id", envOr("NODE_ID", hostname()), "identitas unik node")
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
