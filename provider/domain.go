package provider

import (
	"fmt"
	"strings"
)

// DomainParts contains the normalized domain and its final extension label.
type DomainParts struct {
	Domain    string
	Name      string
	Extension string
}

func parseDomain(input string) (DomainParts, error) {
	domain := strings.ToLower(strings.TrimSpace(input))
	if domain == "" {
		return DomainParts{}, fmt.Errorf("provider: domain is required")
	}
	if len(domain) > 253 || strings.HasSuffix(domain, ".") {
		return DomainParts{}, fmt.Errorf("provider: invalid domain %q", input)
	}

	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return DomainParts{}, fmt.Errorf("provider: domain must contain an extension")
	}
	for _, label := range labels {
		if !validDomainLabel(label) {
			return DomainParts{}, fmt.Errorf("provider: invalid domain %q", input)
		}
	}

	return DomainParts{
		Domain:    domain,
		Name:      strings.Join(labels[:len(labels)-1], "."),
		Extension: labels[len(labels)-1],
	}, nil
}

// ParseDomain validates and splits a domain for registry selection and templates.
func ParseDomain(input string) (DomainParts, error) {
	return parseDomain(input)
}

func validDomainLabel(label string) bool {
	if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for _, char := range label {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
			return false
		}
	}
	return true
}
