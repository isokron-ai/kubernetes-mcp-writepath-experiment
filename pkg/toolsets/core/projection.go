package core

import (
	"fmt"
	"strconv"
	"strings"
)

// projectFields returns the value at each dotted path of obj. A path segment may end
// in [N] (one list element) or [*] (every element); for example
// "spec.template.spec.containers[*].image". A path that does not exist gives nil.
func projectFields(obj map[string]any, paths []string) map[string]any {
	out := make(map[string]any, len(paths))
	for _, path := range paths {
		out[path] = projectPath(obj, strings.Split(path, "."))
	}
	return out
}

func projectPath(value any, segments []string) any {
	if len(segments) == 0 {
		return value
	}
	segment := segments[0]
	key, index := segment, ""
	if i := strings.Index(segment, "["); i >= 0 && strings.HasSuffix(segment, "]") {
		key, index = segment[:i], segment[i+1:len(segment)-1]
	}
	m, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	next, ok := m[key]
	if !ok {
		return nil
	}
	if index == "" {
		return projectPath(next, segments[1:])
	}
	list, ok := next.([]any)
	if !ok {
		return nil
	}
	if index == "*" {
		values := make([]any, 0, len(list))
		for _, item := range list {
			values = append(values, projectPath(item, segments[1:]))
		}
		return values
	}
	i, err := strconv.Atoi(index)
	if err != nil || i < 0 || i >= len(list) {
		return nil
	}
	return projectPath(list[i], segments[1:])
}

// stringList reads an optional array-of-strings argument.
func stringList(args map[string]any, key string) ([]string, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array of strings", key)
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		s, ok := item.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return nil, fmt.Errorf("%s must be an array of non-empty strings", key)
		}
		out = append(out, s)
	}
	return out, nil
}
