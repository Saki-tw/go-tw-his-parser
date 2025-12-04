// 自安裝模組 - 跨平台
// 首次執行時自動安裝到使用者目錄
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	AppName    = "HIS Parser"
	AppID      = "his-parser"
	AppVersion = "1.0.0"
)

// InstallConfig 安裝配置
type InstallConfig struct {
	InstallPath     string // 安裝目錄
	ExePath         string // 執行檔完整路徑
	CreateShortcut  bool   // 是否建立捷徑
	AutoStart       bool   // 是否開機自啟（此應用程式不需要）
}

// GetInstallConfig 取得平台對應的安裝配置
func GetInstallConfig() (*InstallConfig, error) {
	switch runtime.GOOS {
	case "windows":
		return getWindowsInstallConfig()
	case "darwin":
		return getMacOSInstallConfig()
	default: // Linux and others
		return getLinuxInstallConfig()
	}
}

// getWindowsInstallConfig Windows 安裝配置
func getWindowsInstallConfig() (*InstallConfig, error) {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		localAppData = filepath.Join(home, "AppData", "Local")
	}

	installPath := filepath.Join(localAppData, "HISParser")
	return &InstallConfig{
		InstallPath:    installPath,
		ExePath:        filepath.Join(installPath, "his-parser.exe"),
		CreateShortcut: true,
	}, nil
}

// getMacOSInstallConfig macOS 安裝配置
func getMacOSInstallConfig() (*InstallConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	// 使用者 Applications 目錄
	installPath := filepath.Join(home, "Applications", "HIS Parser.app", "Contents", "MacOS")
	return &InstallConfig{
		InstallPath:    installPath,
		ExePath:        filepath.Join(installPath, "his-parser"),
		CreateShortcut: false, // macOS 使用 .app 結構
	}, nil
}

// getLinuxInstallConfig Linux 安裝配置
func getLinuxInstallConfig() (*InstallConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	installPath := filepath.Join(home, ".local", "share", "his-parser")
	return &InstallConfig{
		InstallPath:    installPath,
		ExePath:        filepath.Join(installPath, "his-parser"),
		CreateShortcut: true, // 建立 .desktop 檔案
	}, nil
}

// IsInstalled 檢查是否已安裝
func IsInstalled() bool {
	config, err := GetInstallConfig()
	if err != nil {
		return false
	}

	_, err = os.Stat(config.ExePath)
	return err == nil
}

// IsRunningFromInstallPath 檢查是否從安裝路徑執行
func IsRunningFromInstallPath() bool {
	config, err := GetInstallConfig()
	if err != nil {
		return false
	}

	currentExe, err := os.Executable()
	if err != nil {
		return false
	}

	currentExe, _ = filepath.Abs(currentExe)
	installedPath, _ := filepath.Abs(config.ExePath)

	// 不分大小寫比較（Windows）
	if runtime.GOOS == "windows" {
		return strings.EqualFold(currentExe, installedPath)
	}
	return currentExe == installedPath
}

// Install 執行安裝
func Install() error {
	config, err := GetInstallConfig()
	if err != nil {
		return fmt.Errorf("無法取得安裝配置: %w", err)
	}

	// 建立安裝目錄
	if err := os.MkdirAll(config.InstallPath, 0755); err != nil {
		return fmt.Errorf("無法建立安裝目錄: %w", err)
	}

	// 取得目前執行檔
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("無法取得目前執行檔: %w", err)
	}

	// 複製執行檔
	if err := copyFile(currentExe, config.ExePath); err != nil {
		return fmt.Errorf("無法複製執行檔: %w", err)
	}

	// 設定執行權限（Unix）
	if runtime.GOOS != "windows" {
		os.Chmod(config.ExePath, 0755)
	}

	// 建立平台特定的捷徑
	if config.CreateShortcut {
		if err := createShortcut(config); err != nil {
			// 捷徑建立失敗不影響安裝
			fmt.Printf("建立捷徑失敗（可忽略）: %v\n", err)
		}
	}

	// macOS 特殊處理：建立 .app 結構
	if runtime.GOOS == "darwin" {
		if err := createMacOSApp(config); err != nil {
			fmt.Printf("建立 .app 結構失敗（可忽略）: %v\n", err)
		}
	}

	return nil
}

// copyFile 複製檔案
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	// 先刪除舊檔案
	os.Remove(dst)

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// Uninstall 解除安裝
func Uninstall() error {
	config, err := GetInstallConfig()
	if err != nil {
		return err
	}

	// 移除捷徑
	removeShortcut(config)

	// 移除安裝目錄
	return os.RemoveAll(config.InstallPath)
}

// CheckAndInstall 檢查並安裝（在 main 開頭呼叫）
// 返回 true 表示應該繼續執行，false 表示應該退出（因為已啟動新安裝的版本）
func CheckAndInstall() bool {
	// 如果已經從安裝路徑執行，直接返回
	if IsRunningFromInstallPath() {
		return true
	}

	// 執行安裝
	fmt.Println("首次執行，正在安裝...")
	if err := Install(); err != nil {
		fmt.Printf("安裝失敗: %v\n", err)
		fmt.Println("將以可攜模式執行")
		return true
	}

	fmt.Println("安裝完成！")

	// 啟動已安裝的版本
	config, _ := GetInstallConfig()
	if err := launchInstalled(config.ExePath); err != nil {
		fmt.Printf("無法啟動已安裝版本: %v\n", err)
		return true
	}

	// 結束目前程式（已啟動新版本）
	return false
}
