//go:build !windows

// Unix 平台安裝邏輯 (macOS, Linux)
// - macOS: 建立 .app 結構
// - Linux: 建立 .desktop 捷徑

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// createShortcut 建立 Unix 捷徑
func createShortcut(config *InstallConfig) error {
	if runtime.GOOS == "linux" {
		return createLinuxDesktopEntry(config)
	}
	// macOS 使用 .app 結構，由 createMacOSApp 處理
	return nil
}

// createLinuxDesktopEntry 建立 Linux .desktop 檔案
func createLinuxDesktopEntry(config *InstallConfig) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	// 建立 .desktop 檔案
	desktopDir := filepath.Join(home, ".local", "share", "applications")
	os.MkdirAll(desktopDir, 0755)

	desktopFile := filepath.Join(desktopDir, "his-parser.desktop")

	content := fmt.Sprintf(`[Desktop Entry]
Version=1.0
Type=Application
Name=%s
Comment=台灣醫療資料解析器
Exec="%s"
Icon=utilities-terminal
Terminal=false
Categories=Utility;Office;
StartupNotify=true
`, AppName, config.ExePath)

	if err := os.WriteFile(desktopFile, []byte(content), 0755); err != nil {
		return err
	}

	// 更新桌面資料庫
	exec.Command("update-desktop-database", desktopDir).Run()

	return nil
}

// createMacOSApp 建立 macOS .app 結構
func createMacOSApp(config *InstallConfig) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	// .app 結構
	// ~/Applications/HIS Parser.app/
	//   Contents/
	//     Info.plist
	//     MacOS/
	//       his-parser (執行檔)
	//     Resources/

	appPath := filepath.Join(home, "Applications", "HIS Parser.app")
	contentsPath := filepath.Join(appPath, "Contents")
	macosPath := filepath.Join(contentsPath, "MacOS")
	resourcesPath := filepath.Join(contentsPath, "Resources")

	// 建立目錄結構
	os.MkdirAll(macosPath, 0755)
	os.MkdirAll(resourcesPath, 0755)

	// 建立 Info.plist
	infoPlist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>his-parser</string>
    <key>CFBundleIdentifier</key>
    <string>tw.com.saki-studio.his-parser</string>
    <key>CFBundleName</key>
    <string>%s</string>
    <key>CFBundleDisplayName</key>
    <string>%s</string>
    <key>CFBundleShortVersionString</key>
    <string>%s</string>
    <key>CFBundleVersion</key>
    <string>%s</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleSignature</key>
    <string>????</string>
    <key>LSMinimumSystemVersion</key>
    <string>10.15</string>
    <key>NSHighResolutionCapable</key>
    <true/>
    <key>LSUIElement</key>
    <false/>
</dict>
</plist>
`, AppName, AppName, AppVersion, AppVersion)

	infoPlistPath := filepath.Join(contentsPath, "Info.plist")
	if err := os.WriteFile(infoPlistPath, []byte(infoPlist), 0644); err != nil {
		return fmt.Errorf("無法建立 Info.plist: %w", err)
	}

	return nil
}

// removeShortcut 移除捷徑
func removeShortcut(config *InstallConfig) {
	if runtime.GOOS == "linux" {
		home, err := os.UserHomeDir()
		if err == nil {
			desktopFile := filepath.Join(home, ".local", "share", "applications", "his-parser.desktop")
			os.Remove(desktopFile)
		}
	} else if runtime.GOOS == "darwin" {
		home, err := os.UserHomeDir()
		if err == nil {
			appPath := filepath.Join(home, "Applications", "HIS Parser.app")
			os.RemoveAll(appPath)
		}
	}
}

// launchInstalled 啟動已安裝的版本
func launchInstalled(exePath string) error {
	if runtime.GOOS == "darwin" {
		// macOS: 使用 open 指令開啟 .app
		home, _ := os.UserHomeDir()
		appPath := filepath.Join(home, "Applications", "HIS Parser.app")
		cmd := exec.Command("open", appPath)
		return cmd.Start()
	}

	// Linux: 直接執行
	cmd := exec.Command(exePath)
	return cmd.Start()
}
