package wechat

import (
	"fmt"
	"io/fs"
	"os"
	pathpkg "path"
	"sort"
	"strings"
	"unicode"
)

// SourceRestoreOptions controls the first-stage source restoration pass.
//
// The pass deliberately writes restored files to OutputDir and never removes
// runtime files. Cleanup belongs to a later, success-gated stage.
type SourceRestoreOptions struct {
	OutputDir          string
	BeautifyJavaScript bool
}

// SourceRestoreReport describes the result of restoring JavaScript modules
// from the app-service runtime files.
type SourceRestoreReport struct {
	RuntimeFiles       int
	ModulesFound       int
	FilesWritten       int
	DuplicateModules   int
	ConflictingModules int
	SkippedModules     int
	Warnings           []string
}

type restoredJavaScriptModule struct {
	path       string
	body       string
	sourceFile string
	offset     int
}

// RestoreJavaScriptSource restores JavaScript modules exposed through
// define("path", function (...) { ... }) calls in an unpacked package.
//
// This is intentionally a source-only pass. It does not execute package code,
// does not touch the raw runtime files, and does not remove any intermediate
// files. That makes it safe to validate independently before adding the later
// WXML/WXSS/WXS passes.
func RestoreJavaScriptSource(inputDir string, options SourceRestoreOptions) (SourceRestoreReport, error) {
	var report SourceRestoreReport
	if strings.TrimSpace(inputDir) == "" {
		return report, fmt.Errorf("source restore input directory is empty")
	}
	if info, err := os.Stat(inputDir); err != nil {
		return report, fmt.Errorf("stat source restore input directory: %w", err)
	} else if !info.IsDir() {
		return report, fmt.Errorf("source restore input path is not a directory: %s", inputDir)
	}

	outputDir := options.OutputDir
	if strings.TrimSpace(outputDir) == "" {
		outputDir = inputDir
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return report, fmt.Errorf("create source restore output directory: %w", err)
	}

	runtimeFiles, err := findAppServiceRuntimeFiles(inputDir)
	if err != nil {
		return report, err
	}
	if len(runtimeFiles) == 0 {
		return report, fmt.Errorf("no app-service runtime files found in %s", inputDir)
	}
	report.RuntimeFiles = len(runtimeFiles)

	modules := make(map[string]restoredJavaScriptModule)
	for _, runtimeFile := range runtimeFiles {
		data, err := os.ReadFile(runtimeFile)
		if err != nil {
			return report, fmt.Errorf("read runtime file %s: %w", runtimeFile, err)
		}

		found, warnings := extractJavaScriptModules(string(data), runtimeFile)
		report.Warnings = append(report.Warnings, warnings...)
		report.ModulesFound += len(found)
		for _, module := range found {
			relativePath, err := normalizeRestoredJavaScriptPath(module.path)
			if err != nil {
				report.SkippedModules++
				report.Warnings = append(report.Warnings, fmt.Sprintf("skip module %q from %s: %v", module.path, runtimeFile, err))
				continue
			}
			module.path = relativePath
			if previous, exists := modules[relativePath]; exists {
				report.DuplicateModules++
				if strings.TrimSpace(previous.body) != strings.TrimSpace(module.body) {
					report.ConflictingModules++
					report.Warnings = append(report.Warnings, fmt.Sprintf(
						"module %q has conflicting definitions from %s and %s",
						relativePath, previous.sourceFile, module.sourceFile,
					))
				}
			}
			// Runtime definitions are processed in deterministic source order;
			// the last definition wins, matching the usual module registration
			// behavior when a package contains a repeated module path.
			modules[relativePath] = module
		}
	}

	paths := make([]string, 0, len(modules))
	for relativePath := range modules {
		paths = append(paths, relativePath)
	}
	sort.Strings(paths)

	for _, relativePath := range paths {
		module := modules[relativePath]
		outputPath, err := joinSafeOutputPath(outputDir, relativePath)
		if err != nil {
			report.SkippedModules++
			report.Warnings = append(report.Warnings, fmt.Sprintf("skip module %q: %v", relativePath, err))
			continue
		}
		if err := os.MkdirAll(filepathDir(outputPath), 0o700); err != nil {
			return report, fmt.Errorf("create restored module directory for %s: %w", relativePath, err)
		}

		content := []byte(strings.TrimSpace(module.body))
		if options.BeautifyJavaScript {
			content = PrettyJavaScript(content)
		}
		if err := os.WriteFile(outputPath, content, 0o600); err != nil {
			return report, fmt.Errorf("write restored module %s: %w", outputPath, err)
		}
		report.FilesWritten++
	}

	if report.ModulesFound == 0 {
		return report, fmt.Errorf("no JavaScript modules found in app-service runtime files")
	}
	return report, nil
}

// filepathDir is kept behind a small helper so the virtual path handling in
// this file remains visibly separate from the host filesystem path handling.
func filepathDir(name string) string {
	lastSeparator := strings.LastIndexAny(name, `/\\`)
	if lastSeparator < 0 {
		return "."
	}
	return name[:lastSeparator]
}

func findAppServiceRuntimeFiles(root string) ([]string, error) {
	var files []string
	err := fs.WalkDir(os.DirFS(root), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		base := strings.ToLower(entry.Name())
		if isAppServiceRuntimeFileName(base) {
			files = append(files, name)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("find app-service runtime files: %w", err)
	}

	sort.Slice(files, func(i, j int) bool {
		rankI := runtimeFileRank(files[i])
		rankJ := runtimeFileRank(files[j])
		if rankI != rankJ {
			return rankI < rankJ
		}
		return strings.ToLower(strings.ReplaceAll(files[i], string('\\'), "/")) <
			strings.ToLower(strings.ReplaceAll(files[j], string('\\'), "/"))
	})
	for i, name := range files {
		files[i] = root + string(os.PathSeparator) + strings.ReplaceAll(name, "/", string(os.PathSeparator))
	}
	return files, nil
}

func runtimeFileRank(name string) int {
	base := strings.ToLower(pathpkg.Base(strings.ReplaceAll(name, string('\\'), "/")))
	switch base {
	case "app-service.js":
		return 0
	default:
		return 1
	}
}

func normalizeRestoredJavaScriptPath(name string) (string, error) {
	name = strings.TrimSpace(strings.ReplaceAll(name, string('\\'), "/"))
	if name == "" {
		return "", fmt.Errorf("module path is empty")
	}
	if strings.Contains(name, "\x00") {
		return "", fmt.Errorf("module path contains NUL")
	}
	if strings.Contains(name, "://") {
		return "", fmt.Errorf("module path uses a virtual URI")
	}
	name = strings.TrimPrefix(name, "/")
	name = pathpkg.Clean(name)
	if name == "." || name == ".." || strings.HasPrefix(name, "../") {
		return "", fmt.Errorf("module path escapes output directory")
	}
	if !strings.EqualFold(pathpkg.Ext(name), ".js") {
		return "", fmt.Errorf("module path is not a JavaScript file")
	}
	return name, nil
}

func extractJavaScriptModules(code, sourceFile string) ([]restoredJavaScriptModule, []string) {
	var modules []restoredJavaScriptModule
	var warnings []string
	for i := 0; i < len(code); {
		if isJavaScriptIdentifierAt(code, i, "define") {
			open := skipJavaScriptTrivia(code, i+len("define"))
			if open < len(code) && code[open] == '(' {
				module, next, ok, warning := parseDefineCall(code, open+1, sourceFile, i)
				if warning != "" {
					warnings = append(warnings, warning)
				}
				if ok {
					modules = append(modules, module)
					if next > i {
						i = next
						continue
					}
				}
			}
		}

		next, skipped := skipJavaScriptToken(code, i)
		if skipped {
			i = next
		} else {
			i++
		}
	}
	return modules, warnings
}

func parseDefineCall(code string, start int, sourceFile string, offset int) (restoredJavaScriptModule, int, bool, string) {
	var empty restoredJavaScriptModule
	i := skipJavaScriptTrivia(code, start)
	if i >= len(code) || (code[i] != '\'' && code[i] != '"') {
		// AMD-style define([deps], factory) does not expose a source path.
		return empty, 0, false, ""
	}
	modulePath, next, ok := parseJavaScriptString(code, i)
	if !ok {
		return empty, 0, false, fmt.Sprintf("unable to parse module path in %s at byte %d", sourceFile, offset)
	}
	i = skipJavaScriptTrivia(code, next)
	if i >= len(code) || code[i] != ',' {
		return empty, 0, false, ""
	}
	i = skipJavaScriptTrivia(code, i+1)
	bodyOpen, ok := findFactoryBodyOpen(code, i)
	if !ok {
		return empty, 0, false, ""
	}
	bodyClose, err := findMatchingJavaScriptBrace(code, bodyOpen)
	if err != nil {
		return empty, 0, false, fmt.Sprintf("unable to parse module %q in %s at byte %d: %v", modulePath, sourceFile, offset, err)
	}
	return restoredJavaScriptModule{
		path:       modulePath,
		body:       code[bodyOpen+1 : bodyClose],
		sourceFile: sourceFile,
		offset:     offset,
	}, bodyClose + 1, true, ""
}

func findFactoryBodyOpen(code string, start int) (int, bool) {
	limit := start + 512
	if limit > len(code) {
		limit = len(code)
	}
	hasFunction := false
	hasArrow := false
	for i := start; i < limit; {
		if isJavaScriptIdentifierAt(code, i, "function") {
			hasFunction = true
		}
		if i+2 <= len(code) && code[i:i+2] == "=>" {
			hasArrow = true
		}
		next, skipped := skipJavaScriptToken(code, i)
		if skipped {
			i = next
			continue
		}
		if code[i] == '{' && (hasFunction || hasArrow) {
			return i, true
		}
		i++
	}
	return 0, false
}

func findMatchingJavaScriptBrace(code string, open int) (int, error) {
	depth := 1
	for i := open + 1; i < len(code); {
		next, skipped := skipJavaScriptToken(code, i)
		if skipped {
			i = next
			continue
		}
		switch code[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, nil
			}
		}
		i++
	}
	return 0, fmt.Errorf("unclosed function body")
}

func parseJavaScriptString(code string, start int) (string, int, bool) {
	quote := code[start]
	var builder strings.Builder
	for i := start + 1; i < len(code); i++ {
		if code[i] == quote {
			return builder.String(), i + 1, true
		}
		if code[i] != '\\' {
			builder.WriteByte(code[i])
			continue
		}
		i++
		if i >= len(code) {
			return "", 0, false
		}
		switch code[i] {
		case 'n':
			builder.WriteByte('\n')
		case 'r':
			builder.WriteByte('\r')
		case 't':
			builder.WriteByte('\t')
		case 'b':
			builder.WriteByte('\b')
		case 'f':
			builder.WriteByte('\f')
		case 'v':
			builder.WriteByte('\v')
		case '\\', '\'', '"', '/':
			builder.WriteByte(code[i])
		case '\n':
			// JavaScript permits a line continuation in a string literal.
		case 'x':
			if i+2 >= len(code) {
				return "", 0, false
			}
			value, ok := parseHex(code[i+1 : i+3])
			if !ok {
				return "", 0, false
			}
			builder.WriteByte(byte(value))
			i += 2
		case 'u':
			if i+4 >= len(code) {
				return "", 0, false
			}
			value, ok := parseHex(code[i+1 : i+5])
			if !ok {
				return "", 0, false
			}
			builder.WriteRune(rune(value))
			i += 4
		default:
			builder.WriteByte(code[i])
		}
	}
	return "", 0, false
}

func parseHex(value string) (uint64, bool) {
	var result uint64
	for _, char := range value {
		result <<= 4
		switch {
		case char >= '0' && char <= '9':
			result += uint64(char - '0')
		case char >= 'a' && char <= 'f':
			result += uint64(char-'a') + 10
		case char >= 'A' && char <= 'F':
			result += uint64(char-'A') + 10
		default:
			return 0, false
		}
	}
	return result, true
}

func skipJavaScriptTrivia(code string, start int) int {
	i := start
	for i < len(code) {
		if unicode.IsSpace(rune(code[i])) {
			i++
			continue
		}
		if i+1 < len(code) && code[i:i+2] == "//" {
			i += 2
			for i < len(code) && code[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(code) && code[i:i+2] == "/*" {
			end := strings.Index(code[i+2:], "*/")
			if end < 0 {
				return len(code)
			}
			i += end + 4
			continue
		}
		break
	}
	return i
}

func skipJavaScriptToken(code string, start int) (int, bool) {
	if start >= len(code) {
		return start, false
	}
	if code[start] == '\'' || code[start] == '"' {
		_, end, ok := parseJavaScriptString(code, start)
		if ok {
			return end, true
		}
		return len(code), true
	}
	if code[start] == '`' {
		for i := start + 1; i < len(code); i++ {
			if code[i] == '\\' {
				i++
				continue
			}
			if code[i] == '`' {
				return i + 1, true
			}
		}
		return len(code), true
	}
	if start+1 < len(code) && code[start:start+2] == "//" {
		i := start + 2
		for i < len(code) && code[i] != '\n' {
			i++
		}
		return i, true
	}
	if start+1 < len(code) && code[start:start+2] == "/*" {
		end := strings.Index(code[start+2:], "*/")
		if end < 0 {
			return len(code), true
		}
		return start + end + 4, true
	}
	if code[start] == '/' && looksLikeJavaScriptRegex(code, start) {
		inClass := false
		for i := start + 1; i < len(code); i++ {
			if code[i] == '\\' {
				i++
				continue
			}
			if code[i] == '[' {
				inClass = true
				continue
			}
			if code[i] == ']' {
				inClass = false
				continue
			}
			if code[i] == '/' && !inClass {
				i++
				for i < len(code) && (code[i] == 'g' || code[i] == 'i' || code[i] == 'm' || code[i] == 's' || code[i] == 'u' || code[i] == 'y' || code[i] == 'd') {
					i++
				}
				return i, true
			}
		}
		return len(code), true
	}
	return start, false
}

func isJavaScriptIdentifierAt(code string, start int, identifier string) bool {
	if start < 0 || start+len(identifier) > len(code) || code[start:start+len(identifier)] != identifier {
		return false
	}
	if start > 0 && isJavaScriptIdentifierPart(code[start-1]) {
		return false
	}
	end := start + len(identifier)
	return end == len(code) || !isJavaScriptIdentifierPart(code[end])
}

func isJavaScriptIdentifierPart(value byte) bool {
	return value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' || value == '_' || value == '$'
}

func looksLikeJavaScriptRegex(code string, start int) bool {
	i := start - 1
	for i >= 0 && (code[i] == ' ' || code[i] == '\t' || code[i] == '\r' || code[i] == '\n') {
		i--
	}
	if i < 0 {
		return true
	}
	if strings.ContainsRune("([{=,:;!?&|+-*%^~<>", rune(code[i])) {
		return true
	}
	end := i + 1
	for i >= 0 && isJavaScriptIdentifierPart(code[i]) {
		i--
	}
	word := code[i+1 : end]
	switch word {
	case "return", "throw", "case", "delete", "void", "typeof", "new", "in", "of", "else", "do", "yield", "await":
		return true
	default:
		return false
	}
}
