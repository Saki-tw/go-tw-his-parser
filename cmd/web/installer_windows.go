//go:build windows

// Windows 平台安裝邏輯
// - 建立開始選單捷徑
// - 建立桌面捷徑（可選）
// - 註冊解除安裝項目

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	shell32          = syscall.NewLazyDLL("shell32.dll")
	shGetFolderPathW = shell32.NewProc("SHGetFolderPathW")

	ole32           = syscall.NewLazyDLL("ole32.dll")
	coInitializeEx  = ole32.NewProc("CoInitializeEx")
	coUninitialize  = ole32.NewProc("CoUninitialize")
	coCreateInstance = ole32.NewProc("CoCreateInstance")
)

const (
	CSIDL_PROGRAMS        = 0x0002 // 開始選單\程式集
	CSIDL_DESKTOPDIRECTORY = 0x0010 // 桌面
	CSIDL_STARTMENU       = 0x000b // 開始選單
)

// getSpecialFolderPath 取得特殊資料夾路徑
func getSpecialFolderPath(csidl int) (string, error) {
	buf := make([]uint16, 260)
	ret, _, _ := shGetFolderPathW.Call(0, uintptr(csidl), 0, 0, uintptr(unsafe.Pointer(&buf[0])))
	if ret != 0 {
		return "", fmt.Errorf("SHGetFolderPath 失敗: %d", ret)
	}
	return syscall.UTF16ToString(buf), nil
}

// createShortcut 建立 Windows 捷徑
func createShortcut(config *InstallConfig) error {
	// 取得開始選單程式集路徑
	programsPath, err := getSpecialFolderPath(CSIDL_PROGRAMS)
	if err != nil {
		return err
	}

	// 建立應用程式資料夾
	appFolder := filepath.Join(programsPath, AppName)
	os.MkdirAll(appFolder, 0755)

	// 建立捷徑（使用 PowerShell）
	shortcutPath := filepath.Join(appFolder, AppName+".lnk")

	// PowerShell 腳本建立捷徑
	psScript := fmt.Sprintf(`
$WshShell = New-Object -ComObject WScript.Shell
$Shortcut = $WshShell.CreateShortcut('%s')
$Shortcut.TargetPath = '%s'
$Shortcut.WorkingDirectory = '%s'
$Shortcut.Description = '台灣醫療資料解析器'
$Shortcut.Save()
`, shortcutPath, config.ExePath, config.InstallPath)

	cmd := exec.Command("powershell", "-NoProfile", "-Command", psScript)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("建立開始選單捷徑失敗: %w", err)
	}

	// 建立桌面捷徑
	desktopPath, err := getSpecialFolderPath(CSIDL_DESKTOPDIRECTORY)
	if err == nil {
		desktopShortcut := filepath.Join(desktopPath, AppName+".lnk")
		psScriptDesktop := fmt.Sprintf(`
$WshShell = New-Object -ComObject WScript.Shell
$Shortcut = $WshShell.CreateShortcut('%s')
$Shortcut.TargetPath = '%s'
$Shortcut.WorkingDirectory = '%s'
$Shortcut.Description = '台灣醫療資料解析器'
$Shortcut.Save()
`, desktopShortcut, config.ExePath, config.InstallPath)

		cmd := exec.Command("powershell", "-NoProfile", "-Command", psScriptDesktop)
		cmd.Run() // 桌面捷徑失敗不報錯
	}

	return nil
}

// removeShortcut 移除捷徑
func removeShortcut(config *InstallConfig) {
	// 移除開始選單
	programsPath, err := getSpecialFolderPath(CSIDL_PROGRAMS)
	if err == nil {
		appFolder := filepath.Join(programsPath, AppName)
		os.RemoveAll(appFolder)
	}

	// 移除桌面捷徑
	desktopPath, err := getSpecialFolderPath(CSIDL_DESKTOPDIRECTORY)
	if err == nil {
		desktopShortcut := filepath.Join(desktopPath, AppName+".lnk")
		os.Remove(desktopShortcut)
	}
}

// createMacOSApp Windows 不需要（佔位）
func createMacOSApp(config *InstallConfig) error {
	return nil
}

// launchInstalled 啟動已安裝的版本
func launchInstalled(exePath string) error {
	cmd := exec.Command(exePath)
	return cmd.Start()
}
