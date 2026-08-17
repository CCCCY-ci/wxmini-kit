package wechat

import (
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
)

type componentStaticConfigAssignment struct {
	Path       string
	Expression string
	SourceFile string
}

// restoreStaticConfigJSON recovers the page/component JSON objects emitted as
// __wxCodeSpace__.addComponentStaticConfig(...) calls. These objects are not
// stored as __wxAppCode__ assignments, so looking only for the latter loses
// most page.json files.
func restoreStaticConfigJSON(runtimeCode []string) (map[string][]byte, []string) {
	assignments := make(map[string]componentStaticConfigAssignment)
	var warnings []string
	for _, code := range runtimeCode {
		found, parseWarnings := extractComponentStaticConfigAssignments(code, "runtime")
		warnings = append(warnings, parseWarnings...)
		for _, assignment := range found {
			assignments[assignment.Path] = assignment
		}
	}

	paths := make([]string, 0, len(assignments))
	for key := range assignments {
		paths = append(paths, key)
	}
	sort.Strings(paths)

	result := make(map[string][]byte, len(paths))
	for _, key := range paths {
		assignment := assignments[key]
		value, err := parseStaticJavaScriptExpression(assignment.Expression)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skip static config %q from %s: %v", key, assignment.SourceFile, err))
			continue
		}
		content, err := staticValueToJSON(value)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("serialize static config %q from %s: %v", key, assignment.SourceFile, err))
			continue
		}
		relativePath, err := normalizeStaticConfigJSONPath(key)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skip static config %q: %v", key, err))
			continue
		}
		result[relativePath] = append(content, '\n')
	}
	return result, warnings
}

func extractComponentStaticConfigAssignments(code, sourceFile string) ([]componentStaticConfigAssignment, []string) {
	const marker = "addComponentStaticConfig"
	var assignments []componentStaticConfigAssignment
	var warnings []string
	for offset := 0; offset < len(code); {
		next, skipped := skipJavaScriptToken(code, offset)
		if skipped {
			offset = next
			continue
		}
		if !isJavaScriptIdentifierAt(code, offset, marker) {
			offset++
			continue
		}
		cursor := skipJavaScriptTrivia(code, offset+len(marker))
		if cursor >= len(code) || code[cursor] != '(' {
			offset += len(marker)
			continue
		}
		callEnd, err := findMatchingJavaScriptDelimiter(code, cursor, '(', ')')
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skip static config in %s at byte %d: %v", sourceFile, offset, err))
			break
		}
		arguments, err := splitJavaScriptArguments(code[cursor+1 : callEnd])
		if err != nil || len(arguments) < 2 {
			if err == nil {
				err = fmt.Errorf("expected a path and a config object")
			}
			warnings = append(warnings, fmt.Sprintf("skip static config in %s at byte %d: %v", sourceFile, offset, err))
			offset = callEnd + 1
			continue
		}
		keyValue, err := parseStaticJavaScriptExpression(arguments[0])
		key, ok := keyValue.(string)
		if err != nil || !ok || strings.TrimSpace(key) == "" {
			if err == nil {
				err = fmt.Errorf("config path is not a non-empty string")
			}
			warnings = append(warnings, fmt.Sprintf("skip static config in %s at byte %d: %v", sourceFile, offset, err))
			offset = callEnd + 1
			continue
		}
		assignments = append(assignments, componentStaticConfigAssignment{
			Path:       key,
			Expression: strings.TrimSpace(arguments[1]),
			SourceFile: sourceFile,
		})
		offset = callEnd + 1
	}
	return assignments, warnings
}

func normalizeStaticConfigJSONPath(name string) (string, error) {
	name = strings.TrimSpace(strings.ReplaceAll(name, string(rune(92)), "/"))
	if name == "" {
		return "", fmt.Errorf("config path is empty")
	}
	if strings.EqualFold(path.Ext(name), ".html") {
		name = name[:len(name)-len(".html")]
	}
	if !strings.EqualFold(path.Ext(name), ".json") {
		name += ".json"
	}
	return normalizeCompiledSourcePath(name)
}

func normalizeAppConfigJSON(value staticJavaScriptValue) (staticJavaScriptValue, error) {
	config, ok := value.(map[string]staticJavaScriptValue)
	if !ok {
		return value, fmt.Errorf("app config is %T, not an object", value)
	}
	result := make(map[string]staticJavaScriptValue)
	for _, key := range []string{"pages", "style", "tabBar", "componentFramework", "window"} {
		if item, exists := config[key]; exists {
			result[key] = item
		}
	}
	if global, exists := config["global"]; exists {
		if globalObject, globalOK := global.(map[string]staticJavaScriptValue); globalOK {
			if globalWindow, windowOK := globalObject["window"]; windowOK {
				result["window"] = globalWindow
			} else {
				result["window"] = global
			}
		} else {
			result["window"] = global
		}
	}
	if pages, exists := result["pages"].([]staticJavaScriptValue); exists {
		result["pages"] = normalizeAppConfigPagePaths(pages)
	}
	if tabBar, exists := result["tabBar"].(map[string]staticJavaScriptValue); exists {
		if list, listOK := tabBar["list"].([]staticJavaScriptValue); listOK {
			copyTabBar := make(map[string]staticJavaScriptValue, len(tabBar))
			for key, item := range tabBar {
				copyTabBar[key] = item
			}
			copyTabBar["list"] = normalizeAppConfigTabBarPaths(list)
			result["tabBar"] = copyTabBar
		}
	}
	if framework, exists := result["componentFramework"].(map[string]staticJavaScriptValue); exists {
		if defaultValue, defaultOK := framework["default"].(string); defaultOK && defaultValue != "" {
			result["componentFramework"] = defaultValue
		}
	}
	return result, nil
}

func normalizeAppConfigPagePaths(values []staticJavaScriptValue) []staticJavaScriptValue {
	result := make([]staticJavaScriptValue, len(values))
	for i, value := range values {
		if text, ok := value.(string); ok {
			result[i] = strings.TrimSuffix(text, ".html")
		} else {
			result[i] = value
		}
	}
	return result
}

func normalizeAppConfigTabBarPaths(values []staticJavaScriptValue) []staticJavaScriptValue {
	result := make([]staticJavaScriptValue, len(values))
	for i, value := range values {
		object, ok := value.(map[string]staticJavaScriptValue)
		if !ok {
			result[i] = value
			continue
		}
		copyObject := make(map[string]staticJavaScriptValue, len(object))
		for key, item := range object {
			copyObject[key] = item
		}
		if pagePath, pathOK := copyObject["pagePath"].(string); pathOK {
			copyObject["pagePath"] = strings.TrimSuffix(pagePath, ".html")
		}
		result[i] = copyObject
	}
	return result
}

func restorePackagedAppConfig(root string) ([]byte, error) {
	data, err := os.ReadFile(root + string(os.PathSeparator) + "app-config.json")
	if err != nil {
		return nil, err
	}
	value, err := parseStaticJavaScriptExpression(string(data))
	if err != nil {
		return nil, fmt.Errorf("parse app-config.json: %w", err)
	}
	normalized, err := normalizeAppConfigJSON(value)
	if err != nil {
		return nil, err
	}
	content, err := staticValueToJSON(normalized)
	if err != nil {
		return nil, err
	}
	return append(content, '\n'), nil
}
