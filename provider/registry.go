package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// YAMLProvider is the registrar-neutral provider definition loaded from YAML.
type YAMLProvider struct {
	ID           string             `yaml:"id"`
	Extensions   []string           `yaml:"extensions"`
	Request      YAMLRequest        `yaml:"request"`
	Variables    map[string]string  `yaml:"variables"`
	Response     YAMLResponse       `yaml:"response"`
	Socket       YAMLSocketMessages `yaml:"socket"`
	Debug        bool               `yaml:"debug"`
	DebugLogPath string             `yaml:"debugLogPath"`
}

type YAMLRequest struct {
	Method   string                `yaml:"method"`
	Endpoint string                `yaml:"endpoint"`
	Timeout  time.Duration         `yaml:"timeout"`
	Headers  map[string]string     `yaml:"headers"`
	Query    map[string]string     `yaml:"query"`
	Form     map[string]StringList `yaml:"form"`
	Body     string                `yaml:"body"`
	JSONBody string                `yaml:"json_body"`
}

type YAMLResponse struct {
	Type         string        `yaml:"type"`
	Available    YAMLMatchRule `yaml:"available"`
	NotAvailable YAMLMatchRule `yaml:"not_available"`
	Error        YAMLMatchRule `yaml:"error"`
}

type YAMLMatchRule struct {
	JSONValue        *JSONValueRule    `yaml:"json_value"`
	BodyEquals       string            `yaml:"body_equals"`
	BodyContains     []string          `yaml:"body_contains"`
	BodyNotContains  []string          `yaml:"body_not_contains"`
	HeaderEquals     map[string]string `yaml:"header_equals"`
	HeaderContains   map[string]string `yaml:"header_contains"`
	Regex            string            `yaml:"regex"`
	HTTPStatus       []int             `yaml:"http_status"`
	StatusCodeRanges []StatusCodeRange `yaml:"status_code_ranges"`
}

type YAMLSocketMessages struct {
	Available    string `yaml:"available"`
	NotAvailable string `yaml:"not_available"`
	Error        string `yaml:"error"`
}

// LoadedProvider is a validated YAML provider and its reusable HTTP client.
type LoadedProvider struct {
	ID         string
	Extensions []string
	Socket     YAMLSocketMessages
	Client     *Client
}

// Registry selects a provider by the final domain extension.
type Registry struct {
	providers map[string]*LoadedProvider
}

// CheckResult contains the provider result needed by a socket/core layer.
type CheckResult struct {
	ProviderID     string
	Domain         string
	Result         Result
	SocketResponse string
}

// ProviderCount returns the number of loaded provider definitions.
func (r *Registry) ProviderCount() int {
	if r == nil {
		return 0
	}
	seen := make(map[*LoadedProvider]struct{})
	for _, loaded := range r.providers {
		seen[loaded] = struct{}{}
	}
	return len(seen)
}

// LoadRegistry loads and validates every .yml/.yaml provider in dir.
func LoadRegistry(dir string) (*Registry, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.yml"))
	if err != nil {
		return nil, fmt.Errorf("provider registry: find YAML files: %w", err)
	}
	pathsYAML, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, fmt.Errorf("provider registry: find YAML files: %w", err)
	}
	paths = append(paths, pathsYAML...)
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("provider registry: no provider YAML files in %q", dir)
	}

	registry := &Registry{providers: make(map[string]*LoadedProvider)}
	for _, path := range paths {
		definition, err := loadYAMLProvider(path)
		if err != nil {
			registry.Close()
			return nil, err
		}
		loaded, err := newLoadedProvider(definition)
		if err != nil {
			registry.Close()
			return nil, fmt.Errorf("provider registry: %s: %w", path, err)
		}
		for _, extension := range loaded.Extensions {
			if previous, exists := registry.providers[extension]; exists {
				_ = loaded.Client.Close()
				registry.Close()
				return nil, fmt.Errorf("duplicate extension %q claimed by %q and %q", extension, previous.ID, loaded.ID)
			}
			registry.providers[extension] = loaded
		}
	}
	return registry, nil
}

func loadYAMLProvider(path string) (YAMLProvider, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return YAMLProvider{}, fmt.Errorf("provider registry: read %s: %w", path, err)
	}
	var definition YAMLProvider
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&definition); err != nil {
		return YAMLProvider{}, fmt.Errorf("provider registry: parse %s: %w", path, err)
	}
	return definition, nil
}

func newLoadedProvider(definition YAMLProvider) (*LoadedProvider, error) {
	if strings.TrimSpace(definition.ID) == "" {
		return nil, fmt.Errorf("provider id is required")
	}
	if len(definition.Extensions) == 0 {
		return nil, fmt.Errorf("provider %q has no extensions", definition.ID)
	}
	responseType := strings.ToLower(strings.TrimSpace(definition.Response.Type))
	if responseType != "" && responseType != "json" && responseType != "text" && responseType != "xml" {
		return nil, fmt.Errorf("provider %q: unsupported response type %q", definition.ID, definition.Response.Type)
	}
	extensions := make([]string, 0, len(definition.Extensions))
	for _, extension := range definition.Extensions {
		extension = strings.ToLower(strings.TrimSpace(extension))
		if extension == "" {
			return nil, fmt.Errorf("provider %q has an empty extension", definition.ID)
		}
		if !strings.HasPrefix(extension, ".") {
			extension = "." + extension
		}
		extensions = append(extensions, extension)
	}

	rules := make([]OutcomeRule, 0, 3)
	for _, candidate := range []struct {
		result Result
		rule   YAMLMatchRule
	}{
		{ResultError, definition.Response.Error},
		{ResultAvailable, definition.Response.Available},
		{ResultNotAvailable, definition.Response.NotAvailable},
	} {
		if candidate.rule.hasMatcher() {
			rules = append(rules, toOutcomeRule(candidate.result, candidate.rule))
		}
	}
	client, err := New(Config{
		Method:        definition.Request.Method,
		Endpoint:      definition.Request.Endpoint,
		Timeout:       definition.Request.Timeout,
		Headers:       definition.Request.Headers,
		QueryParams:   definition.Request.Query,
		FormParams:    definition.Request.Form,
		Body:          firstNonEmpty(definition.Request.Body, definition.Request.JSONBody),
		Variables:     definition.Variables,
		OutcomeRules:  rules,
		DefaultResult: ResultError,
		Debug:         definition.Debug,
		DebugLogPath:  definition.DebugLogPath,
	})
	if err != nil {
		return nil, fmt.Errorf("provider %q: %w", definition.ID, err)
	}
	return &LoadedProvider{ID: definition.ID, Extensions: extensions, Socket: definition.Socket, Client: client}, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func toOutcomeRule(result Result, definition YAMLMatchRule) OutcomeRule {
	return OutcomeRule{
		Result:           result,
		StatusCodes:      definition.HTTPStatus,
		StatusCodeRanges: definition.StatusCodeRanges,
		HeaderEquals:     definition.HeaderEquals,
		HeaderContains:   definition.HeaderContains,
		BodyEquals:       definition.BodyEquals,
		BodyContains:     definition.BodyContains,
		BodyNotContains:  definition.BodyNotContains,
		BodyRegex:        definition.Regex,
		JSONValue:        definition.JSONValue,
	}
}

func (r YAMLMatchRule) hasMatcher() bool {
	return r.JSONValue != nil ||
		r.BodyEquals != "" ||
		len(r.BodyContains) > 0 ||
		len(r.BodyNotContains) > 0 ||
		len(r.HeaderEquals) > 0 ||
		len(r.HeaderContains) > 0 ||
		r.Regex != "" ||
		len(r.HTTPStatus) > 0 ||
		len(r.StatusCodeRanges) > 0
}

// Check chooses a provider from the parsed domain extension and checks it.
func (r *Registry) Check(ctx context.Context, domain string) (CheckResult, error) {
	if r == nil {
		return CheckResult{Result: ResultError}, fmt.Errorf("provider registry: nil registry")
	}
	parts, err := ParseDomain(domain)
	if err != nil {
		return CheckResult{Result: ResultError}, err
	}
	key, loaded := r.providerForDomain(parts.Domain)
	if loaded == nil {
		return CheckResult{Domain: parts.Domain, Result: ResultError}, fmt.Errorf("no provider configured for extension")
	}
	parts.Name = strings.TrimSuffix(parts.Domain, key)
	parts.Name = strings.TrimSuffix(parts.Name, ".")
	parts.Extension = strings.TrimPrefix(key, ".")
	result, err := loaded.Client.CheckParts(ctx, parts)
	if err != nil {
		return CheckResult{ProviderID: loaded.ID, Domain: parts.Domain, Result: ResultError, SocketResponse: loaded.Socket.Error}, err
	}
	socketResponse := loaded.Socket.Error
	if result == ResultAvailable {
		socketResponse = loaded.Socket.Available
	} else if result == ResultNotAvailable {
		socketResponse = loaded.Socket.NotAvailable
	}
	return CheckResult{ProviderID: loaded.ID, Domain: parts.Domain, Result: result, SocketResponse: socketResponse}, nil
}

// providerForDomain selects the longest configured suffix. The leading dot
// keeps matching on label boundaries, so .uk cannot match an arbitrary word.
func (r *Registry) providerForDomain(domain string) (string, *LoadedProvider) {
	var selected string
	var loaded *LoadedProvider
	for extension, candidate := range r.providers {
		if strings.HasSuffix(domain, extension) && len(extension) > len(selected) {
			selected = extension
			loaded = candidate
		}
	}
	return selected, loaded
}

// ProviderForDomain returns the selected provider ID and extension.
func (r *Registry) ProviderForDomain(domain string) (string, string, error) {
	parts, err := ParseDomain(domain)
	if err != nil {
		return "", "", err
	}
	extension, loaded := r.providerForDomain(parts.Domain)
	if loaded == nil {
		return "", "", fmt.Errorf("no provider configured for extension")
	}
	return loaded.ID, extension, nil
}

// Close closes debug log files held by loaded providers.
func (r *Registry) Close() error {
	if r == nil {
		return nil
	}
	var firstErr error
	seen := make(map[*LoadedProvider]bool)
	for _, loaded := range r.providers {
		if seen[loaded] {
			continue
		}
		seen[loaded] = true
		if err := loaded.Client.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
