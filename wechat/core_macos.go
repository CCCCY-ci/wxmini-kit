//go:build darwin

package wechat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type macosPlatform struct{}

func newPlatform() platform { return &macosPlatform{} }

func (m *macosPlatform) GetDefaultPaths() PathScanResult {
	var b strings.Builder
	log := func(line string) { b.WriteString(line + "\n") }

	var paths []string

	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		log("获取用户目录失败: " + err.Error())
		return PathScanResult{Paths: paths, Logs: b.String()}
	}

	// ── Step 1: 微信 v3 ──
	v3Path := filepath.Join(userHomeDir, "Library/Containers/com.tencent.xinWeChat/Data/.wxapplet/packages")
	if fileInfo, err := os.Stat(v3Path); err == nil && fileInfo.IsDir() {
		log(fmt.Sprintf("1. 【成功】检测微信 v3 (3.x 版本) - 成功\n   路径: %s", v3Path))
		paths = append(paths, v3Path)
	} else {
		log(fmt.Sprintf("1. 【失败】检测微信 v3 (3.x 版本) - 目录不存在\n   路径: %s", v3Path))
	}

	// ── Step 2: 微信 v4 ──
	v4Path := filepath.Join(userHomeDir, "Library/Containers/com.tencent.xinWeChat/Data/Documents/app_data/radium/Applet/packages")
	if fileInfo, err := os.Stat(v4Path); err == nil && fileInfo.IsDir() {
		log(fmt.Sprintf("2. 【成功】检测微信 v4 (4.x 版本) - 成功\n   路径: %s", v4Path))
		paths = append(paths, v4Path)
	} else {
		log(fmt.Sprintf("2. 【失败】检测微信 v4 (4.x 版本) - 目录不存在\n   路径: %s", v4Path))
	}

	// ── Step 3: 微信 v4 多用户 ──
	v4UsersPath := filepath.Join(userHomeDir, "Library/Containers/com.tencent.xinWeChat/Data/Documents/app_data/radium/users")
	entries, err := os.ReadDir(v4UsersPath)
	if err != nil {
		log(fmt.Sprintf("3. 【失败】检测微信 v4 多用户 (4.x 版本) - 目录不存在\n   路径: %s", v4UsersPath))
	} else {
		var found []string
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			userAppletPath := filepath.Join(v4UsersPath, entry.Name(), "applet", "packages")
			if fileInfo, err := os.Stat(userAppletPath); err == nil && fileInfo.IsDir() {
				paths = append(paths, userAppletPath)
				found = append(found, entry.Name())
			}
		}
		if len(found) == 0 {
			log(fmt.Sprintf("3. 【失败】检测微信 v4 多用户 (4.x 版本) - 未找到有效用户\n   路径: %s", v4UsersPath))
		} else {
			log(fmt.Sprintf("3. 【成功】检测微信 v4 多用户 (4.x 版本) - 成功\n   路径: %s\n   有效用户: %s", v4UsersPath, strings.Join(found, ", ")))
		}
	}

	return PathScanResult{Paths: paths, Logs: b.String()}
}
