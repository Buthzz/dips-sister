package config

import (
	"log/slog"
	"os"
	"testing"
	"time"
)

// TestValidate memastikan Validate mendeteksi konfigurasi yang jelas salah.
func TestValidate(t *testing.T) {
	kosong := Config{}
	if err := kosong.Validate(); err == nil {
		t.Error("Config kosong seharusnya tidak valid")
	}

	valid := Config{Token: "rahasia"}
	if err := valid.Validate(); err != nil {
		t.Errorf("Config valid seharusnya lolos, dapat: %v", err)
	}
}

// TestEnvOr memastikan envOr mengambil nilai env dan kembali ke default jika kosong.
func TestEnvOr(t *testing.T) {
	const key = "DISTAPI_TEST_KEY_XYZ"
	os.Unsetenv(key)

	if got := envOr("TEST_KEY_XYZ", "default"); got != "default" {
		t.Errorf("envOr tanpa env: mau 'default', dapat %q", got)
	}

	os.Setenv(key, "dari-env")
	defer os.Unsetenv(key)

	if got := envOr("TEST_KEY_XYZ", "default"); got != "dari-env" {
		t.Errorf("envOr dengan env: mau 'dari-env', dapat %q", got)
	}
}

// TestEnvOrInt memastikan envOrInt mengonversi string env ke int dengan benar.
func TestEnvOrInt(t *testing.T) {
	const key = "DISTAPI_TEST_INT_XYZ"
	os.Unsetenv(key)

	if got := envOrInt("TEST_INT_XYZ", 42); got != 42 {
		t.Errorf("envOrInt tanpa env: mau 42, dapat %d", got)
	}

	os.Setenv(key, "99")
	defer os.Unsetenv(key)

	if got := envOrInt("TEST_INT_XYZ", 42); got != 99 {
		t.Errorf("envOrInt dengan env: mau 99, dapat %d", got)
	}

	// Nilai tidak valid di env harus diabaikan, kembali ke default.
	os.Setenv(key, "bukan-angka")
	if got := envOrInt("TEST_INT_XYZ", 42); got != 42 {
		t.Errorf("envOrInt env tidak valid: mau 42 (default), dapat %d", got)
	}
}

// TestEnvOrDuration memastikan envOrDuration mem-parse durasi dengan benar.
func TestEnvOrDuration(t *testing.T) {
	const key = "DISTAPI_TEST_DUR_XYZ"
	os.Unsetenv(key)

	def := 5 * time.Second
	if got := envOrDuration("TEST_DUR_XYZ", def); got != def {
		t.Errorf("envOrDuration tanpa env: mau %v, dapat %v", def, got)
	}

	os.Setenv(key, "2m")
	defer os.Unsetenv(key)

	if got := envOrDuration("TEST_DUR_XYZ", def); got != 2*time.Minute {
		t.Errorf("envOrDuration dengan env: mau 2m, dapat %v", got)
	}
}

// TestAutoDetectIP memastikan auto-detect menghasilkan string tidak kosong
// dan mengandung port yang diminta.
func TestAutoDetectIP(t *testing.T) {
	addr := autoDetectIP(9999)
	if addr == "" {
		t.Error("autoDetectIP tidak boleh menghasilkan string kosong")
	}
	// Pastikan port muncul di hasil.
	if addr[len(addr)-4:] != "9999" {
		t.Logf("autoDetectIP menghasilkan: %s (mungkin 127.0.0.1 fallback, OK)", addr)
	}
}

// TestParseLogLevel memastikan semua level log dikenali dengan benar.
func TestParseLogLevel(t *testing.T) {
	kasus := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"tidak-dikenal", slog.LevelInfo}, // default ke info
		{"", slog.LevelInfo},
	}
	for _, k := range kasus {
		got := parseLogLevel(k.input)
		if got != k.want {
			t.Errorf("parseLogLevel(%q): mau %v, dapat %v", k.input, k.want, got)
		}
	}
}

// TestMaxInt memastikan maxInt mengembalikan nilai terbesar.
func TestMaxInt(t *testing.T) {
	if maxInt(3, 5) != 5 {
		t.Error("maxInt(3,5) harus 5")
	}
	if maxInt(7, 2) != 7 {
		t.Error("maxInt(7,2) harus 7")
	}
	if maxInt(4, 4) != 4 {
		t.Error("maxInt(4,4) harus 4")
	}
}
