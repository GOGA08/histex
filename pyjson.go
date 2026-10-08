package main

// pyjson.go - a small JSON writer that reproduces json.dumps() defaults.
//
// Go's encoding/json differs from Python's in two ways that are visible in
// files and stdout: it escapes <, > and & as \u003c ... and it never puts a
// space after ':' or ','. histex wrote config.json, recipes.jsonl and --json
// output with json.dumps(...), so those bytes are reproduced here.

import (
	"strconv"
	"strings"
)

type jpair struct {
	key   string
	value any
}

// jobject is an ordered JSON object (Python dicts keep insertion order).
type jobject []jpair

func pyJSON(value any, indent string, level int) string {
	switch item := value.(type) {
	case nil:
		return "null"
	case bool:
		if item {
			return "true"
		}
		return "false"
	case int:
		return itoa(item)
	case int64:
		return itoa64(item)
	case float64:
		return pyFloat(item)
	case string:
		return pyQuote(item)
	case *string:
		if item == nil {
			return "null"
		}
		return pyQuote(*item)
	case []string:
		return pyArray(stringsToAny(item), indent, level)
	case []any:
		return pyArray(item, indent, level)
	case jobject:
		return pyObject(item, indent, level)
	}
	return "null"
}

func pyArray(items []any, indent string, level int) string {
	if len(items) == 0 {
		return "[]"
	}
	if indent == "" {
		parts := make([]string, 0, len(items))
		for _, item := range items {
			parts = append(parts, pyJSON(item, indent, level+1))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	pad := strings.Repeat(indent, level+1)
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, pad+pyJSON(item, indent, level+1))
	}
	return "[\n" + strings.Join(parts, ",\n") + "\n" + strings.Repeat(indent, level) + "]"
}

func pyObject(pairs jobject, indent string, level int) string {
	if len(pairs) == 0 {
		return "{}"
	}
	if indent == "" {
		parts := make([]string, 0, len(pairs))
		for _, pair := range pairs {
			parts = append(parts, pyQuote(pair.key)+": "+pyJSON(pair.value, indent, level+1))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	pad := strings.Repeat(indent, level+1)
	parts := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		parts = append(parts, pad+pyQuote(pair.key)+": "+pyJSON(pair.value, indent, level+1))
	}
	return "{\n" + strings.Join(parts, ",\n") + "\n" + strings.Repeat(indent, level) + "}"
}

// pyQuote mirrors json.dumps of a str with ensure_ascii=False.
func pyQuote(text string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range text {
		switch r {
		case '"':
			b.WriteString("\\\"")
		case '\\':
			b.WriteString("\\\\")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\t':
			b.WriteString("\\t")
		case '\b':
			b.WriteString("\\b")
		case '\f':
			b.WriteString("\\f")
		default:
			if r < 0x20 {
				b.WriteString("\\u")
				b.WriteString(pad4(lowerHex(int(r))))
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func pad4(hex string) string {
	for len(hex) < 4 {
		hex = "0" + hex
	}
	return hex
}

func lowerHex(value int) string {
	const digits = "0123456789abcdef"
	if value == 0 {
		return "0"
	}
	out := ""
	for value > 0 {
		out = string(digits[value%16]) + out
		value /= 16
	}
	return out
}

func stringsToAny(items []string) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	return out
}

func itoa(value int) string {
	return strconv.FormatInt(int64(value), 10)
}

func itoa64(value int64) string {
	return strconv.FormatInt(value, 10)
}

// pyFloat reproduces json.dumps for a number: a whole float prints plain
// ("168.0" would be Python's float repr, but every numeric config value that
// histex serialises is an int, so whole values stay plain here as well).
func pyFloat(value float64) string {
	if value == float64(int64(value)) {
		return itoa64(int64(value))
	}
	return strconv.FormatFloat(value, 'g', -1, 64)
}
