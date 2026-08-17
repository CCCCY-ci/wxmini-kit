package wechat

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/parser"
)

type CompiledSourceRestoreOptions struct {
	OutputDir    string
	BeautifyJSON bool
	BeautifyWXS  bool
	RestoreWXML  bool
	RestoreWXS   bool
}

type CompiledSourceRestoreReport struct {
	RuntimeFiles int
	JSONFiles    int
	WXSSFiles    int
	WXMLFiles    int
	WXSFiles     int
	InlineWXS    int
	Skipped      int
	Warnings     []string
}

type wxAppCodeAssignment struct {
	Key        string
	Expression string
	SourceFile string
}

type restoredCompiledFile struct {
	Path       string
	Content    []byte
	SourceFile string
}

// RestoreCompiledSources reconstructs the source-facing files that are
// represented as __wxAppCode__ entries in the runtime. It is deliberately
// static: package code is parsed, not executed, so an untrusted package cannot
// run arbitrary application code during a restore.
func RestoreCompiledSources(inputDir string, options CompiledSourceRestoreOptions) (CompiledSourceRestoreReport, error) {
	var report CompiledSourceRestoreReport
	if strings.TrimSpace(inputDir) == "" {
		return report, fmt.Errorf("compiled source restore input directory is empty")
	}
	if info, err := os.Stat(inputDir); err != nil {
		return report, fmt.Errorf("stat compiled source restore input directory: %w", err)
	} else if !info.IsDir() {
		return report, fmt.Errorf("compiled source restore input path is not a directory: %s", inputDir)
	}

	outputDir := options.OutputDir
	if strings.TrimSpace(outputDir) == "" {
		outputDir = inputDir
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return report, fmt.Errorf("create compiled source restore output directory: %w", err)
	}

	runtimeFiles, err := findCompiledSourceRuntimeFiles(inputDir)
	if err != nil {
		return report, err
	}
	if len(runtimeFiles) == 0 {
		return report, fmt.Errorf("no compiled source runtime files found in %s", inputDir)
	}
	report.RuntimeFiles = len(runtimeFiles)

	var assignments []wxAppCodeAssignment
	var runtimeCode []string
	for _, runtimeFile := range runtimeFiles {
		data, err := os.ReadFile(runtimeFile)
		if err != nil {
			return report, fmt.Errorf("read compiled source runtime file %s: %w", runtimeFile, err)
		}
		code := string(data)
		runtimeCode = append(runtimeCode, code)
		assignments = append(assignments, extractWxAppCodeAssignments(code, runtimeFile)...)
		assignments = append(assignments, extractStandaloneWXSSAssignments(code, runtimeFile)...)
	}

	files := make(map[string]restoredCompiledFile)
	for _, assignment := range assignments {
		ext := strings.ToLower(path.Ext(strings.ReplaceAll(assignment.Key, string(rune(92)), "/")))
		switch ext {
		case ".json":
			value, err := parseStaticJavaScriptExpression(assignment.Expression)
			if err != nil {
				report.Skipped++
				report.Warnings = append(report.Warnings, fmt.Sprintf("skip JSON %q from %s: %v", assignment.Key, assignment.SourceFile, err))
				continue
			}
			content, err := staticValueToJSON(value)
			if err != nil {
				report.Skipped++
				report.Warnings = append(report.Warnings, fmt.Sprintf("serialize JSON %q from %s: %v", assignment.Key, assignment.SourceFile, err))
				continue
			}
			content = append(content, '\n')
			relativePath, err := normalizeCompiledSourcePath(assignment.Key)
			if err != nil {
				report.Skipped++
				report.Warnings = append(report.Warnings, fmt.Sprintf("skip JSON %q: %v", assignment.Key, err))
				continue
			}
			if relativePath == "app-config.json" {
				relativePath = "app.json"
				if normalized, normalizeErr := normalizeAppConfigJSON(value); normalizeErr == nil {
					content, normalizeErr = staticValueToJSON(normalized)
					if normalizeErr != nil {
						report.Skipped++
						report.Warnings = append(report.Warnings, fmt.Sprintf("serialize app config from %s: %v", assignment.SourceFile, normalizeErr))
						continue
					}
				} else {
					report.Warnings = append(report.Warnings, fmt.Sprintf("normalize app config from %s: %v", assignment.SourceFile, normalizeErr))
				}
			}
			files[relativePath] = restoredCompiledFile{Path: relativePath, Content: content, SourceFile: assignment.SourceFile}
		case ".wxss":
			content, relativePath, ok, warning := restoreWXSSAssignment(assignment)
			if warning != "" {
				report.Warnings = append(report.Warnings, warning)
			}
			if !ok {
				report.Skipped++
				continue
			}
			files[relativePath] = restoredCompiledFile{Path: relativePath, Content: []byte(content), SourceFile: assignment.SourceFile}
		}
	}

	if appConfig, appConfigErr := restorePackagedAppConfig(inputDir); appConfigErr == nil {
		files["app.json"] = restoredCompiledFile{Path: "app.json", Content: appConfig, SourceFile: "app-config.json"}
	} else if !os.IsNotExist(appConfigErr) {
		report.Warnings = append(report.Warnings, fmt.Sprintf("skip packaged app config: %v", appConfigErr))
	}

	staticConfigFiles, configWarnings := restoreStaticConfigJSON(runtimeCode)
	report.Warnings = append(report.Warnings, configWarnings...)
	for relativePath, content := range staticConfigFiles {
		if _, exists := files[relativePath]; exists {
			continue
		}
		outputPath, err := joinSafeOutputPath(outputDir, relativePath)
		if err != nil {
			report.Skipped++
			report.Warnings = append(report.Warnings, fmt.Sprintf("skip static config %q: %v", relativePath, err))
			continue
		}
		if _, err := os.Stat(outputPath); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			report.Skipped++
			report.Warnings = append(report.Warnings, fmt.Sprintf("skip static config %q: %v", relativePath, err))
			continue
		}
		files[relativePath] = restoredCompiledFile{Path: relativePath, Content: content, SourceFile: "static config"}
	}

	if options.RestoreWXS {
		wxsFiles, inlineWXS, warnings := restoreWXSModules(runtimeCode)
		report.Warnings = append(report.Warnings, warnings...)
		for relativePath, content := range wxsFiles {
			if options.BeautifyWXS {
				content = PrettyJavaScript(content)
			}
			files[relativePath] = restoredCompiledFile{Path: relativePath, Content: content}
		}
		report.InlineWXS = len(inlineWXS)
	}

	paths := make([]string, 0, len(files))
	for relativePath := range files {
		paths = append(paths, relativePath)
	}
	sort.Strings(paths)
	for _, relativePath := range paths {
		file := files[relativePath]
		outputPath, err := joinSafeOutputPath(outputDir, relativePath)
		if err != nil {
			report.Skipped++
			report.Warnings = append(report.Warnings, fmt.Sprintf("skip restored source %q: %v", relativePath, err))
			continue
		}
		if err := os.MkdirAll(filepathDir(outputPath), 0o700); err != nil {
			return report, fmt.Errorf("create restored source directory for %s: %w", relativePath, err)
		}
		if err := os.WriteFile(outputPath, file.Content, 0o600); err != nil {
			return report, fmt.Errorf("write restored source %s: %w", outputPath, err)
		}
		switch strings.ToLower(path.Ext(relativePath)) {
		case ".json":
			report.JSONFiles++
		case ".wxss":
			report.WXSSFiles++
		case ".wxs":
			report.WXSFiles++
		}
	}

	if options.RestoreWXML {
		wxmlFiles, warnings := restoreWXMLSources(runtimeCode)
		report.Warnings = append(report.Warnings, warnings...)
		for relativePath, content := range wxmlFiles {
			outputPath, err := joinSafeOutputPath(outputDir, relativePath)
			if err != nil {
				report.Skipped++
				report.Warnings = append(report.Warnings, fmt.Sprintf("skip restored WXML %q: %v", relativePath, err))
				continue
			}
			if err := os.MkdirAll(filepathDir(outputPath), 0o700); err != nil {
				return report, fmt.Errorf("create restored WXML directory for %s: %w", relativePath, err)
			}
			if err := os.WriteFile(outputPath, []byte(content), 0o600); err != nil {
				return report, fmt.Errorf("write restored WXML %s: %w", outputPath, err)
			}
			report.WXMLFiles++
		}
	}

	return report, nil
}

func findCompiledSourceRuntimeFiles(root string) ([]string, error) {
	files, err := findAppServiceRuntimeFiles(root)
	if err != nil {
		return nil, err
	}
	styleFiles, err := findFilesByBaseName(root, map[string]bool{
		"app-wxss.js": true,
	})
	if err != nil {
		return nil, fmt.Errorf("find compiled style runtime files: %w", err)
	}
	files = append(files, styleFiles...)
	htmlFiles, err := findGeneratedRuntimeHTMLFiles(root)
	if err != nil {
		return nil, fmt.Errorf("find generated runtime HTML files: %w", err)
	}
	files = append(files, htmlFiles...)
	sort.Slice(files, func(i, j int) bool {
		return strings.ToLower(strings.ReplaceAll(files[i], string(rune(92)), "/")) < strings.ToLower(strings.ReplaceAll(files[j], string(rune(92)), "/"))
	})
	return deduplicateStrings(files), nil
}

func findGeneratedRuntimeHTMLFiles(root string) ([]string, error) {
	var files []string
	err := walkDirectoryFiles(root, func(relative, absolute string) error {
		name := path.Base(strings.ReplaceAll(relative, string(rune(92)), "/"))
		if !strings.EqualFold(path.Ext(name), ".html") {
			return nil
		}
		generated, err := isGeneratedRuntimeHTML(absolute)
		if err != nil {
			return err
		}
		if generated {
			files = append(files, absolute)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func findFilesByBaseName(root string, names map[string]bool) ([]string, error) {
	var files []string
	err := walkDirectoryFiles(root, func(relative, absolute string) error {
		if names[strings.ToLower(path.Base(strings.ReplaceAll(relative, string(rune(92)), "/")))] {
			files = append(files, absolute)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func walkDirectoryFiles(root string, callback func(relative, absolute string) error) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		absolute := root + string(os.PathSeparator) + entry.Name()
		if entry.IsDir() {
			if err := walkDirectoryFiles(absolute, func(relative, nestedAbsolute string) error {
				return callback(entry.Name()+"/"+relative, nestedAbsolute)
			}); err != nil {
				return err
			}
			continue
		}
		if err := callback(entry.Name(), absolute); err != nil {
			return err
		}
	}
	return nil
}

func deduplicateStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	return result
}

func extractWxAppCodeAssignments(code, sourceFile string) []wxAppCodeAssignment {
	var assignments []wxAppCodeAssignment
	for offset := 0; offset < len(code); {
		index := strings.Index(code[offset:], "__wxAppCode__")
		if index < 0 {
			break
		}
		index += offset
		if !isJavaScriptIdentifierAt(code, index, "__wxAppCode__") {
			offset = index + len("__wxAppCode__")
			continue
		}
		cursor := skipJavaScriptTrivia(code, index+len("__wxAppCode__"))
		if cursor >= len(code) || code[cursor] != '[' {
			offset = index + len("__wxAppCode__")
			continue
		}
		key, afterKey, ok := parseJavaScriptString(code, skipJavaScriptTrivia(code, cursor+1))
		if !ok {
			offset = cursor + 1
			continue
		}
		cursor = skipJavaScriptTrivia(code, afterKey)
		if cursor >= len(code) || code[cursor] != ']' {
			offset = afterKey
			continue
		}
		cursor = skipJavaScriptTrivia(code, cursor+1)
		if cursor >= len(code) || code[cursor] != '=' {
			offset = cursor + 1
			continue
		}
		cursor = skipJavaScriptTrivia(code, cursor+1)
		end := findJavaScriptExpressionEnd(code, cursor)
		expression := strings.TrimSpace(code[cursor:end])
		if expression != "" {
			assignments = append(assignments, wxAppCodeAssignment{Key: key, Expression: expression, SourceFile: sourceFile})
		}
		offset = end + 1
	}
	return assignments
}

func extractStandaloneWXSSAssignments(code, sourceFile string) []wxAppCodeAssignment {
	var assignments []wxAppCodeAssignment
	for offset := 0; offset < len(code); {
		index := strings.Index(code[offset:], "setCssToHead")
		if index < 0 {
			break
		}
		index += offset
		if !isJavaScriptIdentifierAt(code, index, "setCssToHead") {
			offset = index + len("setCssToHead")
			continue
		}
		cursor := skipJavaScriptTrivia(code, index+len("setCssToHead"))
		if cursor >= len(code) || code[cursor] != '(' {
			offset = index + len("setCssToHead")
			continue
		}
		callEnd, err := findMatchingJavaScriptDelimiter(code, cursor, '(', ')')
		if err != nil {
			break
		}
		arguments, err := splitJavaScriptArguments(code[cursor+1 : callEnd])
		if err == nil && len(arguments) >= 3 {
			if info, infoErr := parseStaticJavaScriptExpression(arguments[2]); infoErr == nil {
				if infoObject, infoOK := info.(map[string]staticJavaScriptValue); infoOK {
					if cssPath, pathOK := infoObject["path"].(string); pathOK &&
						strings.EqualFold(path.Ext(strings.ReplaceAll(cssPath, string(rune(92)), "/")), ".wxss") {
						assignments = append(assignments, wxAppCodeAssignment{
							Key:        strings.TrimPrefix(strings.ReplaceAll(cssPath, string(rune(92)), "/"), "./"),
							Expression: code[index : callEnd+1],
							SourceFile: sourceFile,
						})
					}
				}
			}
		}
		offset = callEnd + 1
	}
	return assignments
}

func normalizeCompiledSourcePath(name string) (string, error) {
	name = strings.TrimSpace(strings.ReplaceAll(name, string(rune(92)), "/"))
	if name == "" {
		return "", fmt.Errorf("source path is empty")
	}
	if strings.HasPrefix(name, "plugin-private://") {
		rest := strings.TrimPrefix(name, "plugin-private://")
		separator := strings.IndexByte(rest, '/')
		if separator <= 0 || separator == len(rest)-1 {
			return "", fmt.Errorf("plugin source path is incomplete")
		}
		pluginID := rest[:separator]
		if !isSafeVirtualPathPart(pluginID) {
			return "", fmt.Errorf("plugin source id is invalid")
		}
		name = path.Join("__plugins__", pluginID, rest[separator+1:])
	} else if strings.Contains(name, "://") {
		return "", fmt.Errorf("source path uses an unsupported virtual URI")
	}
	name = strings.TrimPrefix(name, "/")
	name = path.Clean(name)
	if name == "." || name == ".." || strings.HasPrefix(name, "../") || path.IsAbs(name) {
		return "", fmt.Errorf("source path escapes output directory")
	}
	if len(name) >= 2 && name[1] == ':' {
		return "", fmt.Errorf("source path uses a drive path")
	}
	ext := strings.ToLower(path.Ext(name))
	switch ext {
	case ".json", ".wxss", ".wxml", ".wxs", ".js":
		return name, nil
	default:
		return "", fmt.Errorf("unsupported source extension %q", ext)
	}
}

func isSafeVirtualPathPart(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func restoreWXSSAssignment(assignment wxAppCodeAssignment) (content, relativePath string, ok bool, warning string) {
	expression := strings.TrimSpace(assignment.Expression)
	if !strings.HasPrefix(expression, "setCssToHead") {
		return "", "", false, fmt.Sprintf("skip WXSS %q from %s: unsupported assignment", assignment.Key, assignment.SourceFile)
	}
	open := strings.IndexByte(expression, '(')
	if open < 0 {
		return "", "", false, fmt.Sprintf("skip WXSS %q from %s: missing setCssToHead arguments", assignment.Key, assignment.SourceFile)
	}
	close, err := findMatchingJavaScriptDelimiter(expression, open, '(', ')')
	if err != nil {
		return "", "", false, fmt.Sprintf("skip WXSS %q from %s: %v", assignment.Key, assignment.SourceFile, err)
	}
	arguments, err := splitJavaScriptArguments(expression[open+1 : close])
	if err != nil || len(arguments) == 0 {
		return "", "", false, fmt.Sprintf("skip WXSS %q from %s: invalid setCssToHead arguments", assignment.Key, assignment.SourceFile)
	}
	arrayValue, err := parseStaticJavaScriptExpression(arguments[0])
	if err != nil {
		return "", "", false, fmt.Sprintf("skip WXSS %q from %s: parse CSS array: %v", assignment.Key, assignment.SourceFile, err)
	}
	infoPath := assignment.Key
	suffix := ""
	if len(arguments) >= 3 {
		if info, infoErr := parseStaticJavaScriptExpression(arguments[2]); infoErr == nil {
			if infoObject, infoOK := info.(map[string]staticJavaScriptValue); infoOK {
				if value, pathOK := infoObject["path"].(string); pathOK && value != "" {
					infoPath = value
				}
				if value, suffixOK := infoObject["suffix"].(string); suffixOK {
					suffix = value
				}
			}
		}
	}
	relativePath, err = resolveCompiledStylePath(assignment.Key, infoPath)
	if err != nil {
		return "", "", false, fmt.Sprintf("skip WXSS %q from %s: %v", assignment.Key, assignment.SourceFile, err)
	}
	content, err = cssArrayToText(arrayValue, suffix, relativePath)
	if err != nil {
		return "", "", false, fmt.Sprintf("skip WXSS %q from %s: %v", assignment.Key, assignment.SourceFile, err)
	}
	content = regexp.MustCompile(`body\s*\{`).ReplaceAllString(content, "page{")
	return content, relativePath, true, ""
}

func resolveCompiledStylePath(key, infoPath string) (string, error) {
	if strings.HasPrefix(key, "plugin-private://") {
		prefix := strings.TrimPrefix(key, "plugin-private://")
		separator := strings.IndexByte(prefix, '/')
		if separator <= 0 {
			return "", fmt.Errorf("plugin WXSS path is incomplete")
		}
		pluginID := prefix[:separator]
		relative := strings.TrimPrefix(strings.ReplaceAll(infoPath, string(rune(92)), "/"), "./")
		if relative == "" {
			relative = strings.TrimPrefix(prefix[separator+1:], "./")
		}
		return normalizeCompiledSourcePath("plugin-private://" + pluginID + "/" + relative)
	}
	if strings.TrimSpace(infoPath) != "" {
		if relative, err := normalizeCompiledSourcePath(infoPath); err == nil {
			return relative, nil
		}
	}
	return normalizeCompiledSourcePath(key)
}

func cssArrayToText(value staticJavaScriptValue, suffix, cssPath string) (string, error) {
	items, ok := value.([]staticJavaScriptValue)
	if !ok {
		return "", fmt.Errorf("CSS array is %T, not an array", value)
	}
	var builder strings.Builder
	for _, item := range items {
		if nested, nestedOK := item.([]staticJavaScriptValue); nestedOK {
			if len(nested) < 2 {
				continue
			}
			typeNumber, err := staticNumber(nested[0])
			if err != nil {
				return "", err
			}
			switch int(typeNumber) {
			case 0:
				number, err := staticNumber(nested[1])
				if err != nil {
					continue
				}
				builder.WriteString(strconv.FormatFloat(number, 'f', -1, 64))
				builder.WriteString("rpx")
			case 1:
				builder.WriteString(suffix)
			case 2:
				if importPath, importOK := nested[1].(string); importOK && importPath != "" {
					builder.WriteString(`@import "`)
					builder.WriteString(resolveCSSImportPath(cssPath, importPath))
					builder.WriteString(`";`)
				}
			default:
				builder.WriteString(staticString(nested[1]))
			}
			continue
		}
		if text, textOK := item.(string); textOK {
			builder.WriteString(text)
		}
	}
	return builder.String(), nil
}

func resolveCSSImportPath(cssPath, importPath string) string {
	importPath = strings.ReplaceAll(importPath, string(rune(92)), "/")
	if strings.HasPrefix(importPath, "/") || strings.Contains(importPath, "://") {
		return importPath
	}
	base := path.Dir(cssPath)
	resolved := path.Clean(path.Join(base, importPath))
	return relativeVirtualPath(base, resolved)
}

func relativeVirtualPath(base, target string) string {
	base = path.Clean(strings.ReplaceAll(base, string(rune(92)), "/"))
	target = path.Clean(strings.ReplaceAll(target, string(rune(92)), "/"))
	if base == "." || base == "" {
		return target
	}
	if target == base {
		return "."
	}
	prefix := strings.TrimSuffix(base, "/") + "/"
	if strings.HasPrefix(target, prefix) {
		return strings.TrimPrefix(target, prefix)
	}
	return target
}

var wxsPathMappingPattern = regexp.MustCompile(`['"]([pm]_[^'"]+\.wxs(?:[^'"]*)?)['"]\s*:\s*(np_[A-Za-z0-9_$]+)`)

func restoreWXSModules(runtimeCode []string) (map[string][]byte, map[string][]byte, []string) {
	files := make(map[string][]byte)
	inlineFiles := make(map[string][]byte)
	var warnings []string
	for _, code := range runtimeCode {
		functions := extractNamedFunctionSources(code, "np_")
		if len(functions) == 0 {
			continue
		}
		for _, match := range wxsPathMappingPattern.FindAllStringSubmatch(code, -1) {
			virtualPath, functionName := match[1], match[2]
			functionSource, ok := functions[functionName]
			if !ok {
				warnings = append(warnings, fmt.Sprintf("WXS module %q references missing function %s", virtualPath, functionName))
				continue
			}
			content, err := transformWXSFunction(functionSource)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("skip WXS module %q: %v", virtualPath, err))
				continue
			}
			if strings.HasPrefix(virtualPath, "p_") {
				relativePath, err := normalizeCompiledSourcePath(strings.TrimPrefix(virtualPath, "p_"))
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("skip WXS module %q: %v", virtualPath, err))
					continue
				}
				files[relativePath] = []byte(content)
				continue
			}
			inlineKey := strings.TrimPrefix(virtualPath, "m_")
			separator := strings.LastIndexByte(inlineKey, ':')
			if separator <= 0 || separator == len(inlineKey)-1 {
				warnings = append(warnings, fmt.Sprintf("skip inline WXS module %q: missing owner", virtualPath))
				continue
			}
			owner, err := normalizeCompiledSourcePath(inlineKey[:separator])
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("skip inline WXS module %q: %v", virtualPath, err))
				continue
			}
			inlineFiles[owner+"#"+inlineKey[separator+1:]] = []byte(content)
		}
	}
	return files, inlineFiles, warnings
}

func extractNamedFunctionSources(code, prefix string) map[string]string {
	result := make(map[string]string)
	for offset := 0; offset < len(code); {
		index := strings.Index(code[offset:], "function")
		if index < 0 {
			break
		}
		index += offset
		if !isJavaScriptIdentifierAt(code, index, "function") {
			offset = index + len("function")
			continue
		}
		cursor := skipJavaScriptTrivia(code, index+len("function"))
		nameStart := cursor
		for cursor < len(code) && isJavaScriptIdentifierPart(code[cursor]) {
			cursor++
		}
		name := code[nameStart:cursor]
		if name == "" || !strings.HasPrefix(name, prefix) {
			offset = index + len("function")
			continue
		}
		cursor = skipJavaScriptTrivia(code, cursor)
		if cursor >= len(code) || code[cursor] != '(' {
			offset = index + len("function")
			continue
		}
		parameterEnd, err := findMatchingJavaScriptDelimiter(code, cursor, '(', ')')
		if err != nil {
			break
		}
		bodyOpen := skipJavaScriptTrivia(code, parameterEnd+1)
		if bodyOpen >= len(code) || code[bodyOpen] != '{' {
			offset = parameterEnd + 1
			continue
		}
		bodyClose, err := findMatchingJavaScriptDelimiter(code, bodyOpen, '{', '}')
		if err != nil {
			break
		}
		result[name] = code[index : bodyClose+1]
		offset = bodyClose + 1
	}
	return result
}

func transformWXSFunction(functionSource string) (string, error) {
	const header = "var nv_module={nv_exports:{}};"
	const footer = "return nv_module.nv_exports;"
	start := strings.Index(functionSource, header)
	end := strings.LastIndex(functionSource, footer)
	if start < 0 || end < start {
		return "", fmt.Errorf("unsupported WXS wrapper")
	}
	content := functionSource[start+len(header) : end]
	content = strings.ReplaceAll(content, "nv_", "")
	requirePattern := regexp.MustCompile(`require\((['"])p_(\.[^'"]+)(['"])\)\(\)`)
	content = requirePattern.ReplaceAllStringFunc(content, func(match string) string {
		parts := requirePattern.FindStringSubmatch(match)
		if len(parts) != 4 || parts[1] != parts[3] {
			return match
		}
		return "require('" + strings.TrimPrefix(parts[2], "./") + "')"
	})
	return strings.TrimSpace(content), nil
}

func parseJavaScriptFunctionLiteral(source string) (*ast.FunctionLiteral, error) {
	program, err := parser.ParseFile(nil, "wxml-function.js", "var __fn = "+source+";", 0)
	if err != nil {
		return nil, err
	}
	if len(program.Body) != 1 {
		return nil, fmt.Errorf("function contains unexpected statements")
	}
	statement, ok := program.Body[0].(*ast.VariableStatement)
	if !ok || len(statement.List) != 1 {
		return nil, fmt.Errorf("function did not parse as a variable initializer")
	}
	function, ok := statement.List[0].Initializer.(*ast.FunctionLiteral)
	if !ok {
		return nil, fmt.Errorf("initializer is %T, not a function", statement.List[0].Initializer)
	}
	return function, nil
}
