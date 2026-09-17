package provider

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
)

type debugLogger struct {
	file   *os.File
	logger *log.Logger
}

func newDebugLogger(path string) (*debugLogger, error) {
	if strings.TrimSpace(path) == "" {
		path = "provider-data.log"
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("provider: open debug log: %w", err)
	}
	return &debugLogger{
		file:   file,
		logger: log.New(file, "", log.LstdFlags|log.Lmicroseconds),
	}, nil
}

func (l *debugLogger) Printf(format string, args ...any) {
	if l != nil {
		l.logger.Printf(format, args...)
	}
}

func (l *debugLogger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	return l.file.Close()
}

func (c *Client) debugf(format string, args ...any) {
	if c != nil && c.debugLog != nil {
		c.debugLog.Printf(format, args...)
	}
}

func safeHeaders(headers http.Header) http.Header {
	result := make(http.Header, len(headers))
	for key, values := range headers {
		if sensitiveKey(key) {
			result[key] = []string{"[REDACTED]"}
			continue
		}
		result[key] = append([]string(nil), values...)
	}
	return result
}

func safeRequestBody(req *http.Request) string {
	if req == nil || req.Body == nil || req.GetBody == nil {
		return "[unavailable]"
	}
	body, err := req.GetBody()
	if err != nil {
		return "[unavailable]"
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, 1<<20))
	if err != nil {
		return "[unavailable]"
	}
	return safeBody(req.Header.Get("Content-Type"), string(data))
}

func safeResponseBody(headers http.Header, body string) string {
	return safeBody(headers.Get("Content-Type"), body)
}

func safeBody(contentType, body string) string {
	if strings.HasPrefix(strings.ToLower(contentType), "application/x-www-form-urlencoded") {
		values, err := url.ParseQuery(body)
		if err == nil {
			for key := range values {
				if sensitiveKey(key) {
					values[key] = []string{"[REDACTED]"}
				}
			}
			return values.Encode()
		}
	}
	return redactText(body)
}

var sensitiveTextPattern = regexp.MustCompile(`(?i)("?(?:password|secret|token|api[-_]?key|authorization|cookie)"?\s*[:=]\s*)("[^"]*"|[^,\s}]+)`)

func redactText(body string) string {
	return sensitiveTextPattern.ReplaceAllString(body, `${1}[REDACTED]`)
}

func sensitiveKey(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(key, "-", ""))
	return strings.Contains(key, "password") ||
		strings.Contains(key, "secret") ||
		strings.Contains(key, "token") ||
		key == "authorization" ||
		key == "cookie" ||
		strings.Contains(key, "apikey")
}
