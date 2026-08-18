package graders

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/gronnbeck/habitat/pkg/protocol"
)

// supportedKeywords is the whole schema subset, listed exhaustively rather
// than delegated to a full JSON Schema implementation. A keyword outside this
// set is a load-time error: silently ignoring a misspelled one would leave an
// author believing they have a check they do not have.
var supportedKeywords = map[string]bool{
	"type": true, "required": true, "properties": true, "additionalProperties": true,
	"items": true, "contains": true, "not": true, "enum": true, "const": true,
	"minimum": true, "maximum": true, "exclusiveMinimum": true, "exclusiveMaximum": true,
	"minLength": true, "maxLength": true, "pattern": true,
	"minItems": true, "maxItems": true, "uniqueItems": true, "nullable": true,
}

func init() {
	Register(Grader{
		Name: "json_schema",
		Validate: func(o Options) error {
			if err := o.allow("schema", "path", "source"); err != nil {
				return err
			}
			if err := o.validateSource(); err != nil {
				return err
			}
			schema, ok := normalise(o["schema"]).(map[string]any)
			if !ok {
				return fmt.Errorf("option %q must be an object", "schema")
			}
			return checkSchemaDoc(schema, "schema")
		},
		Grade: gradeSchema,
	})
}

func gradeSchema(res *protocol.Result, o Options) Outcome {
	path, _ := o.str("path")
	root, source := rootFor(res, o)
	label := pathLabel(source, path)
	actual, found := lookupPath(root, path)
	if !found {
		return Outcome{Detail: fmt.Sprintf("no value at %s", label)}
	}
	schema, _ := normalise(o["schema"]).(map[string]any)
	failures := checkValue(normalise(actual), schema, label)
	if len(failures) == 0 {
		return Outcome{Passed: true, Detail: label + " matches schema"}
	}
	return Outcome{Detail: strings.Join(failures, "; ")}
}

// checkSchemaDoc walks a schema at load time, rejecting unsupported keywords.
func checkSchemaDoc(schema map[string]any, where string) error {
	var unknown []string
	for keyword := range schema {
		if !supportedKeywords[keyword] {
			unknown = append(unknown, keyword)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("%s: unsupported schema keyword(s) %s", where, strings.Join(unknown, ", "))
	}
	if pattern, ok := schema["pattern"].(string); ok {
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("%s: invalid pattern: %w", where, err)
		}
	}
	return checkNestedSchemas(schema, where)
}

func checkNestedSchemas(schema map[string]any, where string) error {
	for _, keyword := range []string{"items", "contains", "not", "additionalProperties"} {
		nested, ok := schema[keyword].(map[string]any)
		if !ok {
			continue
		}
		if err := checkSchemaDoc(nested, where+"."+keyword); err != nil {
			return err
		}
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil
	}
	for name, nested := range properties {
		sub, ok := nested.(map[string]any)
		if !ok {
			continue
		}
		if err := checkSchemaDoc(sub, where+".properties."+name); err != nil {
			return err
		}
	}
	return nil
}

// checkValue returns the reasons a value fails a schema, empty when it passes.
func checkValue(value any, schema map[string]any, path string) []string {
	if schema == nil {
		return nil
	}
	if value == nil && truthy(schema["nullable"]) {
		return nil
	}
	var failures []string
	failures = append(failures, checkType(value, schema, path)...)
	failures = append(failures, checkEnumConst(value, schema, path)...)
	failures = append(failures, checkNumeric(value, schema, path)...)
	failures = append(failures, checkString(value, schema, path)...)
	failures = append(failures, checkArray(value, schema, path)...)
	failures = append(failures, checkObject(value, schema, path)...)
	failures = append(failures, checkNot(value, schema, path)...)
	return failures
}

func checkNot(value any, schema map[string]any, path string) []string {
	nested, ok := schema["not"].(map[string]any)
	if !ok {
		return nil
	}
	if len(checkValue(value, nested, path)) == 0 {
		return []string{path + " matches a schema it must not match"}
	}
	return nil
}

func checkType(value any, schema map[string]any, path string) []string {
	expected, ok := schema["type"]
	if !ok {
		return nil
	}
	names := typeNames(expected)
	if len(names) == 0 {
		return nil
	}
	actual := jsonTypeOf(value)
	for _, name := range names {
		if name == actual || (name == "number" && actual == "integer") {
			return nil
		}
	}
	return []string{fmt.Sprintf("%s: expected type %s, got %s", path, strings.Join(names, "|"), actual)}
}

func typeNames(expected any) []string {
	switch t := expected.(type) {
	case string:
		return []string{t}
	case []any:
		names := make([]string, 0, len(t))
		for _, item := range t {
			if name, ok := item.(string); ok {
				names = append(names, name)
			}
		}
		return names
	default:
		return nil
	}
}

func jsonTypeOf(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		if f, ok := toFloat(v); ok {
			if f == float64(int64(f)) {
				return "integer"
			}
			return "number"
		}
		return "unknown"
	}
}

func checkEnumConst(value any, schema map[string]any, path string) []string {
	var failures []string
	if allowed, ok := schema["enum"].([]any); ok {
		if !anyEqual(allowed, value) {
			failures = append(failures, fmt.Sprintf("%s: %s is not one of %s", path, describe(value), describe(allowed)))
		}
	}
	if expected, ok := schema["const"]; ok && !equalValues(expected, value) {
		failures = append(failures, fmt.Sprintf("%s: expected %s, got %s", path, describe(expected), describe(value)))
	}
	return failures
}

func anyEqual(candidates []any, value any) bool {
	for _, candidate := range candidates {
		if equalValues(candidate, value) {
			return true
		}
	}
	return false
}

func checkNumeric(value any, schema map[string]any, path string) []string {
	actual, ok := toFloat(value)
	if !ok {
		return nil
	}
	bounds := []struct {
		keyword string
		ok      func(limit float64) bool
		message string
	}{
		{"minimum", func(l float64) bool { return actual >= l }, "must be >="},
		{"maximum", func(l float64) bool { return actual <= l }, "must be <="},
		{"exclusiveMinimum", func(l float64) bool { return actual > l }, "must be >"},
		{"exclusiveMaximum", func(l float64) bool { return actual < l }, "must be <"},
	}
	var failures []string
	for _, bound := range bounds {
		limit, present := toFloat(schema[bound.keyword])
		if present && !bound.ok(limit) {
			failures = append(failures, fmt.Sprintf("%s: %v %s %v", path, actual, bound.message, limit))
		}
	}
	return failures
}

func checkString(value any, schema map[string]any, path string) []string {
	text, ok := value.(string)
	if !ok {
		return nil
	}
	var failures []string
	if limit, present := toFloat(schema["minLength"]); present && float64(len(text)) < limit {
		failures = append(failures, fmt.Sprintf("%s: shorter than minLength %v", path, limit))
	}
	if limit, present := toFloat(schema["maxLength"]); present && float64(len(text)) > limit {
		failures = append(failures, fmt.Sprintf("%s: longer than maxLength %v", path, limit))
	}
	pattern, ok := schema["pattern"].(string)
	if !ok {
		return failures
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return append(failures, fmt.Sprintf("%s: invalid pattern %q", path, pattern))
	}
	if !re.MatchString(text) {
		failures = append(failures, fmt.Sprintf("%s: %s does not match /%s/", path, describe(text), pattern))
	}
	return failures
}

func checkArray(value any, schema map[string]any, path string) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	var failures []string
	if limit, present := toFloat(schema["minItems"]); present && float64(len(items)) < limit {
		failures = append(failures, fmt.Sprintf("%s: has %d items, minItems %v", path, len(items), limit))
	}
	if limit, present := toFloat(schema["maxItems"]); present && float64(len(items)) > limit {
		failures = append(failures, fmt.Sprintf("%s: has %d items, maxItems %v", path, len(items), limit))
	}
	if truthy(schema["uniqueItems"]) && hasDuplicate(items) {
		failures = append(failures, path+": items are not unique")
	}
	failures = append(failures, checkEachItem(items, schema, path)...)
	return append(failures, checkContains(items, schema, path)...)
}

func checkEachItem(items []any, schema map[string]any, path string) []string {
	nested, ok := schema["items"].(map[string]any)
	if !ok {
		return nil
	}
	var failures []string
	for i, item := range items {
		failures = append(failures, checkValue(item, nested, fmt.Sprintf("%s[%d]", path, i))...)
	}
	return failures
}

// checkContains is how presence inside an unordered list is asserted: at
// least one item must match, unlike items, where every one must.
func checkContains(items []any, schema map[string]any, path string) []string {
	nested, ok := schema["contains"].(map[string]any)
	if !ok {
		return nil
	}
	for _, item := range items {
		if len(checkValue(item, nested, path)) == 0 {
			return nil
		}
	}
	return []string{path + ": no item matches the contains schema"}
}

func hasDuplicate(items []any) bool {
	for i := range items {
		for j := i + 1; j < len(items); j++ {
			if equalValues(items[i], items[j]) {
				return true
			}
		}
	}
	return false
}

func checkObject(value any, schema map[string]any, path string) []string {
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	failures := checkRequired(object, schema, path)
	properties, _ := schema["properties"].(map[string]any)
	for name, nested := range properties {
		sub, ok := nested.(map[string]any)
		if !ok {
			continue
		}
		if present, exists := object[name]; exists {
			failures = append(failures, checkValue(present, sub, path+"."+name)...)
		}
	}
	return append(failures, checkAdditional(object, schema, properties, path)...)
}

func checkRequired(object, schema map[string]any, path string) []string {
	required, ok := schema["required"].([]any)
	if !ok {
		return nil
	}
	var failures []string
	for _, item := range required {
		name, ok := item.(string)
		if !ok {
			continue
		}
		if _, exists := object[name]; !exists {
			failures = append(failures, fmt.Sprintf("%s: missing required property %q", path, name))
		}
	}
	return failures
}

func checkAdditional(object, schema, properties map[string]any, path string) []string {
	additional, present := schema["additionalProperties"]
	if !present {
		return nil
	}
	allowed, isBool := additional.(bool)
	if !isBool || allowed {
		return nil
	}
	var extra []string
	for name := range object {
		if _, declared := properties[name]; !declared {
			extra = append(extra, name)
		}
	}
	if len(extra) == 0 {
		return nil
	}
	sort.Strings(extra)
	return []string{fmt.Sprintf("%s: unexpected propert(ies) %s", path, strings.Join(extra, ", "))}
}

func truthy(v any) bool {
	b, ok := v.(bool)
	return ok && b
}
