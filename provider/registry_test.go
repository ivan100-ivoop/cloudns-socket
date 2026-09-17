package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistryCloudNSDynamicJSONValue(t *testing.T) {
	responses := []struct {
		name     string
		body     string
		expected Result
	}{
		{name: "available", body: `{"example.info":1}`, expected: ResultAvailable},
		{name: "not available", body: `{"example.info":0}`, expected: ResultNotAvailable},
		{name: "nested available", body: `{"example.info":{"status":1}}`, expected: ResultAvailable},
		{name: "nested not available", body: `{"example.info":{"status":0}}`, expected: ResultNotAvailable},
		{name: "error", body: `{"status":"Failed","statusDescription":"Invalid authentication, incorrect auth-id or auth-password."}`, expected: ResultError},
	}

	for _, test := range responses {
		t.Run(test.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.body))
			}))
			defer srv.Close()

			dir := t.TempDir()
			jsonPath := "{{domain}}"
			if strings.Contains(test.body, `{"example.info":{"`) {
				jsonPath = "{{domain}}.status"
			}
			config := fmt.Sprintf(`
id: cloudns
extensions: [.info]
request:
  method: POST
  endpoint: %q
  timeout: 2s
  form:
    auth-id: "{{auth_id}}"
    auth-password: "{{auth_password}}"
    name: "{{domain_name}}"
    tld[]: "{{tld}}"
variables:
  auth_id: test-id
  auth_password: test-password
response:
  type: json
  error:
    body_contains: ['"status":"Failed"']
  available:
    json_value:
      path: %q
      equals: 1
  not_available:
    json_value:
      path: %q
      equals: 0
socket:
  available: CLOUDNS_AVAILABLE
  not_available: CLOUDNS_NOT_AVAILABLE
  error: CLOUDNS_ERROR
`, srv.URL, jsonPath, jsonPath)
			if err := os.WriteFile(filepath.Join(dir, "cloudns.yml"), []byte(config), 0600); err != nil {
				t.Fatalf("write config: %v", err)
			}

			registry, err := LoadRegistry(dir)
			if err != nil {
				t.Fatalf("LoadRegistry() error: %v", err)
			}
			defer registry.Close()

			result, err := registry.Check(context.Background(), "example.info")
			if err != nil {
				t.Fatalf("Check() error: %v", err)
			}
			if result.Result != test.expected || result.ProviderID != "cloudns" {
				t.Fatalf("unexpected result: %+v", result)
			}
		})
	}
}

func TestLoadRegistryAcceptsHeaderAndBodyMatchers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Status", "available")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("example.info is available"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	config := fmt.Sprintf(`
id: cloudns
extensions: [.info]
request:
  method: GET
  endpoint: %q
  timeout: 1s
response:
  type: text
  available:
    status_code_ranges:
      - min: 200
        max: 299
    header_equals:
      X-Status: available
    body_equals: "example.info is available"
  not_available:
    body_contains: ["taken"]
  error:
    body_contains: ["error"]
socket:
  available: AVAILABLE
  not_available: NOT_AVAILABLE
  error: ERROR
`, srv.URL)
	if err := os.WriteFile(filepath.Join(dir, "cloudns.yml"), []byte(config), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	registry, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry() error: %v", err)
	}
	defer registry.Close()

	result, err := registry.Check(context.Background(), "example.info")
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}
	if result.Result != ResultAvailable {
		t.Fatalf("unexpected classification: %+v", result)
	}
}

func TestLoadRegistryRejectsDuplicateExtensions(t *testing.T) {
	dir := t.TempDir()
	config := func(id string) string {
		return fmt.Sprintf("id: %s\nextensions: [.info]\nrequest:\n  method: GET\n  endpoint: http://127.0.0.1\n  timeout: 1s\n", id)
	}
	for _, file := range []struct{ name, id string }{{"one.yml", "one"}, {"two.yml", "two"}} {
		if err := os.WriteFile(filepath.Join(dir, file.name), []byte(config(file.id)), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}

	if _, err := LoadRegistry(dir); err == nil {
		t.Fatal("LoadRegistry() expected duplicate extension error")
	}
}
