package provider

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

func matchRegex(pattern, value string) (bool, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false, err
	}
	return re.MatchString(value), nil
}

func matchJSONValue(body string, rule *JSONValueRule, data renderData) (bool, error) {
	if rule == nil {
		return true, nil
	}
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return false, err
	}
	path, err := renderString(rule.Path, data)
	if err != nil {
		return false, err
	}
	actual, ok := jsonPathValue(document, path)
	if !ok {
		return false, nil
	}
	return jsonValuesEqual(actual, rule.Equals), nil
}

func jsonPathValue(document any, path string) (any, bool) {
	if object, ok := document.(map[string]any); ok {
		if value, exists := object[path]; exists {
			return value, true
		}
	}
	parts := strings.Split(strings.Trim(path, "."), ".")
	current := document
	for index := 0; index < len(parts); {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		found := false
		for end := len(parts); end > index; end-- {
			key := strings.Join(parts[index:end], ".")
			if value, exists := object[key]; exists {
				current = value
				index = end
				found = true
				break
			}
		}
		if !found {
			return nil, false
		}
	}
	return current, true
}

func jsonValuesEqual(left, right any) bool {
	leftNumber, leftOK := numberValue(left)
	rightNumber, rightOK := numberValue(right)
	if leftOK || rightOK {
		return leftOK && rightOK && leftNumber == rightNumber
	}
	return reflect.DeepEqual(left, right)
}

func numberValue(value any) (float64, bool) {
	if number, ok := value.(json.Number); ok {
		parsed, err := strconv.ParseFloat(number.String(), 64)
		return parsed, err == nil
	}
	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return 0, false
	}
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	}
	return 0, false
}
