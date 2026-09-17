package gateway

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivan100-ivoop/cloudns-socket/provider"
)

func TestTCPServerProviderResponsesAndLongestExtension(t *testing.T) {
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error: %v", err)
		}
		name := r.Form.Get("name")
		tld := r.Form.Get("tld[]")
		domain := name + "." + tld
		w.Header().Set("Content-Type", "application/json")
		if name == "registered" {
			fmt.Fprintf(w, `{"%s":0}`, domain)
			return
		}
		fmt.Fprintf(w, `{"%s":1}`, domain)
	}))
	defer httpServer.Close()

	configDir := t.TempDir()
	providersDir := filepath.Join(configDir, "providers")
	if err := os.Mkdir(providersDir, 0700); err != nil {
		t.Fatalf("Mkdir() error: %v", err)
	}
	for _, definition := range []struct {
		file, id, extension, available string
	}{
		{"info.yml", "cloudns", ".info", "CLOUDNS_AVAILABLE"},
		{"uk.yml", "uk-provider", ".uk", "UK_AVAILABLE"},
		{"couk.yml", "co-uk-provider", ".co.uk", "COUK_AVAILABLE"},
	} {
		content := fmt.Sprintf(`id: %s
extensions: [%s]
request:
  method: POST
  endpoint: %q
  timeout: 2s
  form:
    name: "{{domain_name}}"
    tld[]: "{{tld}}"
response:
  type: json
  available:
    json_value:
      path: "{{domain}}"
      equals: 1
  not_available:
    json_value:
      path: "{{domain}}"
      equals: 0
socket:
  available: %s
  not_available: NOT_AVAILABLE
  error: ERROR
`, definition.id, definition.extension, httpServer.URL, definition.available)
		if err := os.WriteFile(filepath.Join(providersDir, definition.file), []byte(content), 0600); err != nil {
			t.Fatalf("WriteFile() error: %v", err)
		}
	}

	registry, err := provider.LoadRegistry(providersDir)
	if err != nil {
		t.Fatalf("LoadRegistry() error: %v", err)
	}
	defer registry.Close()

	cfg := Config{ProvidersPath: "providers", Server: ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    time.Second,
		WriteTimeout:   time.Second,
		IdleTimeout:    time.Second,
		MaxRequestSize: 512,
		ErrorResponse:  "DOMAIN_GATEWAY_ERROR",
	}}
	server, err := NewServer(cfg, registry, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error: %v", err)
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()

	assertResponse := func(domain, expected string) {
		t.Helper()
		conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
		if err != nil {
			t.Fatalf("Dial() error: %v", err)
		}
		defer conn.Close()
		if _, err := fmt.Fprintf(conn, "%s\r\n", domain); err != nil {
			t.Fatalf("write request: %v", err)
		}
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			t.Fatalf("read response: %v", err)
		}
		if strings.TrimSpace(line) != expected {
			t.Fatalf("domain=%s response=%q, want %q", domain, line, expected)
		}
	}

	assertResponse("example.info", "CLOUDNS_AVAILABLE")
	assertResponse("registered.info", "NOT_AVAILABLE")
	assertResponse("example.invalidtld", "DOMAIN_GATEWAY_ERROR")
	assertResponse("example.co.uk", "COUK_AVAILABLE")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() error: %v", err)
	}
	if err := <-serveErr; err != ErrServerClosed {
		t.Fatalf("Serve() error: %v", err)
	}
}

func TestTCPServerRejectsIPOutsideAllowlist(t *testing.T) {
	cfg := Config{
		ProvidersPath: "providers",
		Server: ServerConfig{
			Host:           "127.0.0.1",
			Port:           0,
			ReadTimeout:    time.Second,
			WriteTimeout:   time.Second,
			IdleTimeout:    time.Second,
			MaxRequestSize: 512,
			ErrorResponse:  "DOMAIN_GATEWAY_ERROR",
			AllowedIPs:     []string{"192.0.2.1/32"},
		},
	}
	server, err := NewServer(cfg, &provider.Registry{}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error: %v", err)
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()

	conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatalf("Dial() error: %v", err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, readErr := conn.Read(make([]byte, 1))
	if readErr == nil {
		t.Fatal("expected denied connection to close")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() error: %v", err)
	}
	if err := <-serveErr; err != ErrServerClosed {
		t.Fatalf("Serve() error: %v", err)
	}
}
