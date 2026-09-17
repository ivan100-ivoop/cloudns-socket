package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseDomain(t *testing.T) {
	parts, err := parseDomain(" App.INFO ")
	if err != nil {
		t.Fatalf("parseDomain() error: %v", err)
	}
	if parts.Domain != "app.info" || parts.Name != "app" || parts.Extension != "info" {
		t.Fatalf("unexpected domain parts: %+v", parts)
	}
}

func TestClientRejectsUnsafeDomain(t *testing.T) {
	client, err := New(Config{
		Method:   http.MethodGet,
		Endpoint: "http://127.0.0.1:1",
		Timeout:  time.Second,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	result, err := client.Check(context.Background(), "app.info/path?x=1")
	if err == nil {
		t.Fatal("Check() expected validation error")
	}
	if result != ResultError {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestClientCheckAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("domain"); got != "example.com" {
			t.Fatalf("unexpected domain query: %q", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"free"}`))
	}))
	defer srv.Close()

	client, err := New(Config{
		Method:   http.MethodGet,
		Endpoint: srv.URL,
		Timeout:  2 * time.Second,
		QueryParams: map[string]string{
			"domain": "{{.Domain}}",
		},
		OutcomeRules: []OutcomeRule{
			{
				Result:      ResultAvailable,
				StatusCodes: []int{http.StatusOK},
				BodyContains: []string{
					`"status":"free"`,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	result, err := client.Check(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}
	if result != ResultAvailable {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestClientCheckFormParameterList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error: %v", err)
		}
		got := r.Form["tld"]
		if len(got) != 2 || got[0] != "info" || got[1] != "com" {
			t.Fatalf("unexpected tld values: %#v", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client, err := New(Config{
		Method:   http.MethodPost,
		Endpoint: srv.URL,
		Timeout:  2 * time.Second,
		FormParams: map[string]StringList{
			"tld": {"{{.Extension}}", "com"},
		},
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	result, err := client.Check(context.Background(), "app.info")
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}
	if result != ResultError {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestClientCheckFallbackResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`unexpected`))
	}))
	defer srv.Close()

	client, err := New(Config{
		Method:        http.MethodGet,
		Endpoint:      srv.URL,
		Timeout:       2 * time.Second,
		DefaultResult: ResultError,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	result, err := client.Check(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}
	if result != ResultError {
		t.Fatalf("unexpected result: %s", result)
	}
}
