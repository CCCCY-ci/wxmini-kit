package wechat

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const unpackedRuntimeDirectory = ".wxapkg-runtime"

type unpackSourceRestoreResult struct {
	files    int
	warnings int
}

func stripRuntimeDuplicateSuffix(name string) string {
	name = strings.ToLower(filepath.Base(name))
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for {
		dash := strings.LastIndexByte(stem, '-')
		if dash <= 0 || dash == len(stem)-1 {
			break
		}
		suffix := stem[dash+1:]
		if suffix == "" {
			break
		}
		numeric := true
		for _, char := range suffix {
			if char < '0' || char > '9' {
				numeric = false
				break
			}
		}
		if !numeric {
			break
		}
		stem = stem[:dash]
	}
	return stem + ext
}

// restoreUnpackedSources runs the source-facing restoration pass after the
// archive entries have been written. Runtime files are removed after the
// reconstructed source tree has been written.
func (u *Unpacker) restoreUnpackedSources() unpackSourceRestoreResult {
	var result unpackSourceRestoreResult
	if u == nil || u.item == nil || strings.TrimSpace(u.item.UnpackSavePath) == "" {
		return result
	}
	root := u.item.UnpackSavePath

	if !u.isCancelled() {
		if runtimeFiles, err := findAppServiceRuntimeFiles(root); err != nil {
			result.warnings++
		} else if len(runtimeFiles) > 0 {
			report, restoreErr := RestoreJavaScriptSource(root, SourceRestoreOptions{
				OutputDir:          root,
				BeautifyJavaScript: u.options != nil && u.options.EnableJsBeautify,
			})
			result.files += report.FilesWritten
			result.warnings += len(report.Warnings)
			if restoreErr != nil {
				result.warnings++
			}
		}
	}

	if !u.isCancelled() {
		if runtimeFiles, err := findCompiledSourceRuntimeFiles(root); err != nil {
			result.warnings++
		} else if len(runtimeFiles) > 0 {
			report, restoreErr := RestoreCompiledSources(root, CompiledSourceRestoreOptions{
				OutputDir:   root,
				BeautifyWXS: u.options != nil && u.options.EnableJsBeautify,
				RestoreWXML: true,
				RestoreWXS:  true,
			})
			result.files += report.JSONFiles + report.WXSSFiles + report.WXMLFiles + report.WXSFiles
			result.warnings += len(report.Warnings)
			if restoreErr != nil {
				result.warnings++
			}
		}
	}

	if u.isCancelled() {
		return result
	}
	_, runtimeWarnings := removeUnpackedRuntimeFiles(root)
	result.warnings += runtimeWarnings
	_, duplicateWarnings := removeIdenticalDuplicateFiles(root)
	result.warnings += duplicateWarnings
	return result
}

func isAppServiceRuntimeFileName(name string) bool {
	name = stripRuntimeDuplicateSuffix(name)
	return name == "app-service.js" ||
		name == "appservice.app.js" ||
		strings.HasSuffix(name, ".appservice.js")
}

func isGeneratedRuntimeFileName(name string) bool {
	name = stripRuntimeDuplicateSuffix(name)
	return isAppServiceRuntimeFileName(name) ||
		name == "app-wxss.js" ||
		name == "app-config.json" ||
		name == "common.app.js" ||
		name == "webview.app.js" ||
		name == "page-frame.js" ||
		strings.EqualFold(filepath.Ext(name), ".fpcssb") ||
		strings.EqualFold(filepath.Ext(name), ".fpiib") ||
		strings.HasSuffix(name, ".webview.js")
}

func isGeneratedRuntimeHTML(filePath string) (bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, 128*1024))
	if err != nil {
		return false, err
	}
	source := strings.ToLower(string(data))
	legacyRuntime := strings.Contains(source, "__wxappcodereadycallback__") &&
		(strings.Contains(source, "$gwx_") || strings.Contains(source, "__wxappcode__"))
	glassEaselRuntime := strings.Contains(source, "__wxcodespace__") &&
		(strings.Contains(source, "batchaddcompiledtemplate") ||
			strings.Contains(source, "addcomponentstaticconfig") ||
			strings.Contains(source, "__wxappcode__"))
	return legacyRuntime || glassEaselRuntime, nil
}

func isGeneratedRuntimeFile(filePath, name string) (bool, error) {
	if isGeneratedRuntimeFileName(name) {
		return true, nil
	}
	if !strings.EqualFold(filepath.Ext(name), ".html") {
		return false, nil
	}
	return isGeneratedRuntimeHTML(filePath)
}

func findRuntimeFilesForRemoval(root string) ([]string, error) {
	runtimeRoot := filepath.Clean(filepath.Join(root, unpackedRuntimeDirectory))
	var files []string
	err := filepath.WalkDir(root, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filepath.Clean(filePath) == runtimeRoot {
			return fs.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		generated, err := isGeneratedRuntimeFile(filePath, entry.Name())
		if err != nil {
			return err
		}
		if generated {
			files = append(files, filePath)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func removeUnpackedRuntimeFiles(root string) (int, int) {
	files, err := findRuntimeFilesForRemoval(root)
	if err != nil {
		return 0, 1
	}

	removed := 0
	warnings := 0
	for _, sourcePath := range files {
		if err := os.Remove(sourcePath); err != nil {
			warnings++
			continue
		}
		removed++
	}
	legacyRuntimeRoot := filepath.Join(root, unpackedRuntimeDirectory)
	if _, err := os.Lstat(legacyRuntimeRoot); err == nil {
		if err := os.RemoveAll(legacyRuntimeRoot); err != nil {
			warnings++
		}
	} else if !os.IsNotExist(err) {
		warnings++
	}
	return removed, warnings
}

func duplicateBasePath(filePath string) (string, bool) {
	name := filepath.Base(filePath)
	baseName := stripRuntimeDuplicateSuffix(name)
	if strings.EqualFold(baseName, name) {
		return "", false
	}
	return filepath.Join(filepath.Dir(filePath), baseName), true
}

func removeIdenticalDuplicateFiles(root string) (int, int) {
	runtimeRoot := filepath.Clean(filepath.Join(root, unpackedRuntimeDirectory))
	var candidates []string
	err := filepath.WalkDir(root, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filepath.Clean(filePath) == runtimeRoot {
			return fs.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		if _, ok := duplicateBasePath(filePath); ok {
			candidates = append(candidates, filePath)
		}
		return nil
	})
	if err != nil {
		return 0, 1
	}

	sort.Strings(candidates)
	removed := 0
	warnings := 0
	for _, sourcePath := range candidates {
		basePath, ok := duplicateBasePath(sourcePath)
		if !ok {
			continue
		}
		sourceInfo, err := os.Stat(sourcePath)
		if err != nil {
			if !os.IsNotExist(err) {
				warnings++
			}
			continue
		}
		baseInfo, err := os.Stat(basePath)
		if err != nil || !baseInfo.Mode().IsRegular() || sourceInfo.Size() != baseInfo.Size() {
			continue
		}
		sourceData, err := os.ReadFile(sourcePath)
		if err != nil {
			warnings++
			continue
		}
		baseData, err := os.ReadFile(basePath)
		if err != nil || !bytes.Equal(sourceData, baseData) {
			continue
		}
		if err := os.Remove(sourcePath); err != nil {
			warnings++
			continue
		}
		removed++
	}
	return removed, warnings
}

func sourceRestoreSummary(result unpackSourceRestoreResult) string {
	if result.files == 0 && result.warnings == 0 {
		return ""
	}
	if result.warnings == 0 {
		return fmt.Sprintf("source restored: %d files", result.files)
	}
	return fmt.Sprintf("source restored: %d files, %d warnings", result.files, result.warnings)
}
