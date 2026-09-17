package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// Result is the standardized outcome returned by the provider.
type Result string

const (
	ResultAvailable    Result = "AVAILABLE"
	ResultNotAvailable Result = "NOT_AVAILABLE"
	ResultError        Result = "ERROR"
)

// Config defines a fully config-driven HTTP provider.
// All request details and response classification rules come from configuration.
type Config struct {
	Method          string                `yaml:"method" json:"method"`
	Endpoint        string                `yaml:"endpoint" json:"endpoint"`
	Timeout         time.Duration         `yaml:"timeout" json:"timeout"`
	Headers         map[string]string     `yaml:"headers" json:"headers"`
	QueryParams     map[string]string     `yaml:"queryParams" json:"queryParams"`
	FormParams      map[string]StringList `yaml:"formParams" json:"formParams"`
	Body            string                `yaml:"body" json:"body"`
	Variables       map[string]string     `yaml:"variables" json:"variables"`
	OutcomeRules    []OutcomeRule         `yaml:"outcomeRules" json:"outcomeRules"`
	DefaultResult   Result                `yaml:"defaultResult" json:"defaultResult"`
	FollowRedirects bool                  `yaml:"followRedirects" json:"followRedirects"`
	Debug           bool                  `yaml:"debug" json:"debug"`
	DebugLogPath    string                `yaml:"debugLogPath" json:"debugLogPath"`
}

// StringList accepts either one configured value or a list of values.
// Lists are encoded as repeated form fields with the same key.
type StringList []string

func (v *StringList) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		var value string
		if err := node.Decode(&value); err != nil {
			return err
		}
		*v = StringList{value}
		return nil
	}
	if node.Kind != yaml.SequenceNode {
		return fmt.Errorf("expected string or list of strings")
	}

	var values []string
	if err := node.Decode(&values); err != nil {
		return fmt.Errorf("decode string list: %w", err)
	}
	*v = StringList(values)
	return nil
}

func (v *StringList) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err == nil {
		*v = StringList{value}
		return nil
	}

	var values []string
	if err := json.Unmarshal(bytes.TrimSpace(data), &values); err != nil {
		return fmt.Errorf("expected string or list of strings: %w", err)
	}
	*v = StringList(values)
	return nil
}

// OutcomeRule describes how to classify a response.
// The first matching rule wins.
type OutcomeRule struct {
	Result           Result            `yaml:"result" json:"result"`
	StatusCodes      []int             `yaml:"statusCodes" json:"statusCodes"`
	StatusCodeRanges []StatusCodeRange `yaml:"statusCodeRanges" json:"statusCodeRanges"`
	HeaderEquals     map[string]string `yaml:"headerEquals" json:"headerEquals"`
	HeaderContains   map[string]string `yaml:"headerContains" json:"headerContains"`
	BodyEquals       string            `yaml:"bodyEquals" json:"bodyEquals"`
	BodyContains     []string          `yaml:"bodyContains" json:"bodyContains"`
	BodyNotContains  []string          `yaml:"bodyNotContains" json:"bodyNotContains"`
	BodyRegex        string            `yaml:"bodyRegex" json:"bodyRegex"`
	JSONValue        *JSONValueRule    `yaml:"jsonValue" json:"jsonValue"`
}

// JSONValueRule matches a value at a dynamic dot-separated JSON path.
type JSONValueRule struct {
	Path   string `yaml:"path" json:"path"`
	Equals any    `yaml:"equals" json:"equals"`
}

// StatusCodeRange matches inclusive HTTP status code intervals.
type StatusCodeRange struct {
	Min int `yaml:"min" json:"min"`
	Max int `yaml:"max" json:"max"`
}

// Response captures the HTTP response used for rule matching.
type Response struct {
	StatusCode int
	Headers    map[string][]string
	Body       string
}
