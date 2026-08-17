package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/wux1an/wxapkg/wechat"
)

type AppService struct {
	ctx     context.Context
	items   sync.Map // map[string]*wechat.WxapkgItem
	cancels sync.Map // map[string]*unpackTask
}

type unpackTask struct {
	cancel context.CancelFunc
}

func NewAppService() *AppService {
	return &AppService{}
}

func (a *AppService) startup(ctx context.Context) {
	a.ctx = ctx
}

// storeItem stores the item and returns a pointer to it for modification
func (a *AppService) storeItem(item *wechat.WxapkgItem) *wechat.WxapkgItem {
	a.items.Store(item.UUID, item)
	return item
}

// GetWxapkgItem returns the latest state of the item by UUID (push-pull pattern)
func (a *AppService) GetWxapkgItem(uuid string) *wechat.WxapkgItem {
	if val, ok := a.items.Load(uuid); ok {
		return val.(*wechat.WxapkgItem)
	}
	return nil
}

func (a *AppService) OpenDirectoryDialog(title string, root string) (string, error) {
	if _, err := os.Stat(root); os.IsNotExist(err) {
		root = ""
	}
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            title,
		DefaultDirectory: root,
	})
}

func (a *AppService) OpenFileDialog(title string, root string, filters []FileFilter) (string, error) {
	if _, err := os.Stat(root); os.IsNotExist(err) {
		root = ""
	}
	goFilters := make([]runtime.FileFilter, len(filters))
	for i, f := range filters {
		goFilters[i] = runtime.FileFilter{DisplayName: f.DisplayName, Pattern: f.Pattern}
	}
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            title,
		DefaultDirectory: root,
		Filters:          goFilters,
	})
}

func (a *AppService) GetDefaultPaths() wechat.PathScanResult {
	return wechat.Platform.GetDefaultPaths()
}

func (a *AppService) ScanWxapkgItem(path string, scan bool) ([]wechat.WxapkgItem, error) {
	//return a.generateWxapkgItemDemo()
	return wechat.ScanWxapkgItem(path, scan)
}

func (a *AppService) generateWxapkgItemDemo() ([]wechat.WxapkgItem, error) {
	generateWxId := func() string {
		hexChars := "0123456789abcdef"
		id := "wx"
		for i := 0; i < 16; i++ {
			id += string(hexChars[rand.IntN(16)])
		}
		return id
	}

	items := make([]wechat.WxapkgItem, 10)
	startTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	endTime := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC).Unix()

	for i := 0; i < 10; i++ {
		wxId := generateWxId()
		location := filepath.Join("C:\\Users\\Example\\Documents\\WeChat Files\\Applet", wxId)

		randomTime := startTime + rand.Int64N(endTime-startTime)

		item := wechat.WxapkgItem{
			UUID:           fmt.Sprintf("demo-%d", i+1),
			WxId:           wxId,
			Location:       location,
			Size:           int64(rand.IntN(5*1024*1024) + 1024*1024),
			IsDir:          true,
			LastModifyTime: randomTime,
		}

		items[i] = item
	}

	slices.SortFunc(items, func(e wechat.WxapkgItem, e2 wechat.WxapkgItem) int {
		return (int)(e2.LastModifyTime - e.LastModifyTime)
	})

	items[0].UnpackStatus = wechat.StatusTypeFinished
	items[1].UnpackStatus = wechat.StatusTypeRunning

	return items, nil
}

func (a *AppService) ClipboardSetText(text string) error {
	return runtime.ClipboardSetText(a.ctx, text)
}

func (a *AppService) UnpackWxapkgItem(item wechat.WxapkgItem, options wechat.UnpackOptions) {
	ctx, cancel := context.WithCancel(context.Background())
	task := &unpackTask{cancel: cancel}
	a.cancels.Store(item.UUID, task)
	defer a.cancels.CompareAndDelete(item.UUID, task)
	defer cancel()

	itemPtr := a.storeItem(&item)
	wechat.NewUnpackerWithCancel(itemPtr, &options, ctx.Done()).UnpackWithStatusCallback(func(item *wechat.WxapkgItem) {
		a.items.Store(item.UUID, item)
		runtime.EventsEmit(a.ctx, "unpack:progress-changed", item.UUID)
	})
}

// CancelUnpack requests cancellation of a running unpack task.
func (a *AppService) CancelUnpack(uuid string) bool {
	if value, ok := a.cancels.Load(uuid); ok {
		value.(*unpackTask).cancel()
		return true
	}
	return false
}

func (a *AppService) Version() string {
	return version
}

func (a *AppService) Github() string {
	return github
}

func (a *AppService) OpenUrl(url string) error {
	runtime.BrowserOpenURL(a.ctx, url)
	return nil
}

func (a *AppService) OpenPath(path string) error {
	if path == "" {
		return errors.New("this path is empty")
	}
	var opener = map[string]string{
		"windows": "explorer",
		"darwin":  "open",
		"linux":   "xdg-open",
	}
	cmd, ok := opener[goruntime.GOOS]
	if !ok {
		return fmt.Errorf("unsupported OS: %s", goruntime.GOOS)
	}
	return exec.Command(cmd, path).Start()
}

func (a *AppService) ComputeSavePath(outputDir string, wxapkgPath string) string {
	absDir, err := filepath.Abs(outputDir)
	if err != nil {
		return outputDir
	}
	baseName := filepath.Base(wxapkgPath)
	ext := filepath.Ext(baseName)
	if ext != "" {
		baseName = strings.TrimSuffix(baseName, ext)
	}
	if baseName == "" || baseName == "." || baseName == string(filepath.Separator) {
		baseName = "unpacked"
	}

	for suffix := 1; ; suffix++ {
		candidateName := baseName
		if suffix > 1 {
			candidateName = fmt.Sprintf("%s_%d", baseName, suffix)
		}
		candidate := filepath.Join(absDir, candidateName)
		if pathsEqual(candidate, wxapkgPath) || pathIsOccupied(candidate) {
			continue
		}
		return candidate
	}
}

// pathIsOccupied only reports entries that are known to exist. Other stat
// errors are passed to the unpacker, which can return the actionable
// permission or filesystem error instead of making path allocation loop.
func pathIsOccupied(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func pathsEqual(left string, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	leftAbs = filepath.Clean(leftAbs)
	rightAbs = filepath.Clean(rightAbs)
	if goruntime.GOOS == "windows" {
		return strings.EqualFold(leftAbs, rightAbs)
	}
	return leftAbs == rightAbs
}

type FileFilter struct {
	DisplayName string
	Pattern     string
}

func (a *AppService) shutdown() {}

func (a *AppService) beforeClose() {}
