//go:build windows

package wechat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

type windowsPlatform struct{}

func newPlatform() platform { return &windowsPlatform{} }

func (m *windowsPlatform) GetDefaultPaths() PathScanResult {
	var b strings.Builder
	log := func(line string) { b.WriteString(line + "\n") }

	var paths []string

	// ── Step 1: 微信 v4 根目录 ──
	appDataDir, _ := os.UserConfigDir()
	v4Path := filepath.Join(appDataDir, "Tencent", "xwechat", "radium", "Applet", "packages")
	if fileInfo, err := os.Stat(v4Path); err == nil && fileInfo.IsDir() {
		log(fmt.Sprintf("1. 【成功】检测微信 v4 (4.x 版本) - 成功\n   路径: %s", v4Path))
		paths = append(paths, v4Path)
	} else {
		log(fmt.Sprintf("1. 【失败】检测微信 v4 (4.x 版本) - 目录不存在\n   路径: %s", v4Path))
	}

	// ── Step 2: 微信 v3 注册表 ──
	wechatKey, err := registry.OpenKey(registry.CURRENT_USER, `Software\Tencent\WeChat`, registry.QUERY_VALUE)
	if err != nil {
		log("2. 【失败】检测微信 v3 (3.x 版本) - 未找到注册表项\n   键: HKCU\\Software\\Tencent\\WeChat\\FileSavePath")
	} else {
		defer wechatKey.Close()
		value, _, err := wechatKey.GetStringValue("FileSavePath")
		if err != nil {
			log("2. 【失败】检测微信 v3 (3.x 版本) - 未找到注册表值\n   键: HKCU\\Software\\Tencent\\WeChat\\FileSavePath")
		} else {
			if value == "MyDocument:" {
				value = filepath.Join(os.Getenv("USERPROFILE"), "Documents")
			}
			v3Path := filepath.Join(value, "WeChat Files")
			if fileInfo, err := os.Stat(v3Path); err == nil && fileInfo.IsDir() {
				log(fmt.Sprintf("2. 【成功】检测微信 v3 (3.x 版本) - 成功\n   键: HKCU\\Software\\Tencent\\WeChat\\FileSavePath\n   路径: %s", v3Path))
				paths = append(paths, v3Path)
			} else {
				log(fmt.Sprintf("2. 【失败】检测微信 v3 (3.x 版本) - 目录不存在\n   键: HKCU\\Software\\Tencent\\WeChat\\FileSavePath\n   路径: %s", v3Path))
			}
		}
	}

	// ── Step 3: 微信 v4 多用户 ──
	usersDir := filepath.Join(appDataDir, "Tencent", "xwechat", "radium", "users")
	entries, err := os.ReadDir(usersDir)
	if err != nil || entries == nil {
		log(fmt.Sprintf("3. 【失败】检测微信 v4 多用户 (4.x 版本) - 目录不存在\n   路径: %s", usersDir))
	} else {
		var found []string
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			userDir := filepath.Join(usersDir, entry.Name(), "applet", "packages")
			if fileInfo, err := os.Stat(userDir); err == nil && fileInfo.IsDir() {
				paths = append(paths, userDir)
				found = append(found, entry.Name())
			}
		}
		if len(found) == 0 {
			log(fmt.Sprintf("3. 【失败】检测微信 v4 多用户 (4.x 版本) - 未找到有效用户\n   路径: %s", usersDir))
		} else {
			log(fmt.Sprintf("3. 【成功】检测微信 v4 多用户 (4.x 版本) - 成功\n   路径: %s\n   有效用户: %s", usersDir, strings.Join(found, ", ")))
		}
	}

	return PathScanResult{Paths: paths, Logs: b.String()}
}
