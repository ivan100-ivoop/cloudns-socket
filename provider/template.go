package provider

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"
	"text/template"
)

func renderString(input string, data renderData) (string, error) {
	funcs := template.FuncMap{
		"lower":       strings.ToLower,
		"upper":       strings.ToUpper,
		"trimSpace":   strings.TrimSpace,
		"urlquery":    url.QueryEscape,
		"domain":      func() string { return data.Domain },
		"domain_name": func() string { return data.Name },
		"tld":         func() string { return data.Extension },
	}
	for key, value := range data.Values {
		name := key
		value := value
		if isTemplateIdentifier(name) {
			funcs[name] = func() string { return value }
		}
	}
	tmpl, err := template.New("provider").Funcs(funcs).Option("missingkey=error").Parse(input)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	ctx := struct {
		Domain    string
		Name      string
		Extension string
		TLD       string
		Values    map[string]string
	}{
		Domain:    data.Domain,
		Name:      data.Name,
		Extension: data.Extension,
		TLD:       data.Extension,
		Values:    withDefaultValues(data.Values),
	}
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}

func isTemplateIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for i, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && char != '_' && (i == 0 || char < '0' || char > '9') {
			return false
		}
	}
	return true
}
