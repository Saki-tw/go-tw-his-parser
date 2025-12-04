// 自動更新模組
// 背景檢查 GitHub Releases，提示使用者更新
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	UpdateRepoOwner     = "Saki-tw"
	UpdateRepoName      = "go-tw-his-parser"
	UpdateCheckInterval = 24 * time.Hour
	GitHubAPIBase       = "https://api.github.com"
)

// Updater 自動更新管理器
type Updater struct {
	currentVersion string
	latestRelease  *GitHubRelease
	downloadURL    string
	downloadedPath string
	checkTime      time.Time
	isChecking     bool
	isDownloading  bool
	downloadProgress float64
	lastError      error
	mu             sync.RWMutex
}

// GitHubRelease GitHub Release 結構
type GitHubRelease struct {
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	Body        string        `json:"body"`
	Draft       bool          `json:"draft"`
	Prerelease  bool          `json:"prerelease"`
	PublishedAt string        `json:"published_at"`
	Assets      []GitHubAsset `json:"assets"`
	HTMLURL     string        `json:"html_url"`
}

// GitHubAsset GitHub Release Asset
type GitHubAsset struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// UpdateStatus 更新狀態
type UpdateStatus struct {
	CurrentVersion   string `json:"current_version"`
	LatestVersion    string `json:"latest_version,omitempty"`
	UpdateAvailable  bool   `json:"update_available"`
	IsChecking       bool   `json:"is_checking"`
	IsDownloading    bool   `json:"is_downloading"`
	DownloadProgress float64 `json:"download_progress,omitempty"`
	DownloadReady    bool   `json:"download_ready"`
	DownloadURL      string `json:"download_url,omitempty"`
	ReleaseNotes     string `json:"release_notes,omitempty"`
	ReleaseURL       string `json:"release_url,omitempty"`
	Error            string `json:"error,omitempty"`
}

// NewUpdater 建立更新管理器
func NewUpdater(version string) *Updater {
	return &Updater{
		currentVersion: normalizeVersion(version),
	}
}

// Start 啟動背景更新檢查
func (u *Updater) Start() {
	go func() {
		// 延遲 10 秒後首次檢查
		time.Sleep(10 * time.Second)
		u.CheckForUpdate()

		// 定期檢查
		ticker := time.NewTicker(UpdateCheckInterval)
		defer ticker.Stop()
		for range ticker.C {
			u.CheckForUpdate()
		}
	}()
}

// CheckForUpdate 檢查是否有新版本
func (u *Updater) CheckForUpdate() error {
	u.mu.Lock()
	if u.isChecking {
		u.mu.Unlock()
		return fmt.Errorf("正在檢查中")
	}
	u.isChecking = true
	u.lastError = nil
	u.mu.Unlock()

	defer func() {
		u.mu.Lock()
		u.isChecking = false
		u.checkTime = time.Now()
		u.mu.Unlock()
	}()

	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest",
		GitHubAPIBase, UpdateRepoOwner, UpdateRepoName)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		u.setError(err)
		return err
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "HIS-Parser/"+u.currentVersion)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		u.setError(err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil // 沒有 release
	}

	if resp.StatusCode != 200 {
		err = fmt.Errorf("GitHub API 回傳 %d", resp.StatusCode)
		u.setError(err)
		return err
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		u.setError(err)
		return err
	}

	if release.Draft || release.Prerelease {
		return nil
	}

	// 找到對應平台的下載連結
	downloadURL := u.findAssetURL(release.Assets)

	u.mu.Lock()
	u.latestRelease = &release
	u.downloadURL = downloadURL
	u.mu.Unlock()

	return nil
}

// findAssetURL 根據平台找到下載連結
func (u *Updater) findAssetURL(assets []GitHubAsset) string {
	osName := runtime.GOOS
	archName := runtime.GOARCH

	// 檔名對應
	expectedNames := []string{}

	switch osName {
	case "windows":
		expectedNames = append(expectedNames,
			"his-parser-web-windows.exe",
			fmt.Sprintf("his-parser-web-windows-%s.exe", archName),
		)
	case "darwin":
		expectedNames = append(expectedNames,
			fmt.Sprintf("his-parser-web-darwin-%s", archName),
			"his-parser-web-darwin-arm64",
			"his-parser-web-darwin-amd64",
		)
	case "linux":
		expectedNames = append(expectedNames,
			fmt.Sprintf("his-parser-web-linux-%s", archName),
			"his-parser-web-linux-amd64",
		)
	}

	for _, asset := range assets {
		assetLower := strings.ToLower(asset.Name)
		for _, expected := range expectedNames {
			if strings.ToLower(expected) == assetLower {
				return asset.BrowserDownloadURL
			}
		}
	}

	return ""
}

// IsUpdateAvailable 檢查是否有更新
func (u *Updater) IsUpdateAvailable() bool {
	u.mu.RLock()
	defer u.mu.RUnlock()

	if u.latestRelease == nil {
		return false
	}

	latestVersion := normalizeVersion(u.latestRelease.TagName)
	return compareVersions(latestVersion, u.currentVersion) > 0
}

// GetStatus 取得更新狀態
func (u *Updater) GetStatus() UpdateStatus {
	u.mu.RLock()
	defer u.mu.RUnlock()

	status := UpdateStatus{
		CurrentVersion:   u.currentVersion,
		IsChecking:       u.isChecking,
		IsDownloading:    u.isDownloading,
		DownloadProgress: u.downloadProgress,
		DownloadReady:    u.downloadedPath != "",
	}

	if u.latestRelease != nil {
		latestVersion := normalizeVersion(u.latestRelease.TagName)
		status.LatestVersion = latestVersion
		status.UpdateAvailable = compareVersions(latestVersion, u.currentVersion) > 0
		status.ReleaseNotes = u.latestRelease.Body
		status.ReleaseURL = u.latestRelease.HTMLURL
		status.DownloadURL = u.downloadURL
	}

	if u.lastError != nil {
		status.Error = u.lastError.Error()
	}

	return status
}

// DownloadUpdate 下載更新
func (u *Updater) DownloadUpdate() error {
	u.mu.Lock()
	if u.isDownloading {
		u.mu.Unlock()
		return fmt.Errorf("正在下載中")
	}
	if u.downloadURL == "" {
		u.mu.Unlock()
		return fmt.Errorf("沒有可用的下載連結")
	}
	u.isDownloading = true
	u.downloadProgress = 0
	downloadURL := u.downloadURL
	u.mu.Unlock()

	defer func() {
		u.mu.Lock()
		u.isDownloading = false
		u.mu.Unlock()
	}()

	// 下載到臨時目錄
	tempDir := os.TempDir()
	filename := filepath.Base(downloadURL)
	downloadPath := filepath.Join(tempDir, "his-parser-update", filename)
	os.MkdirAll(filepath.Dir(downloadPath), 0755)

	resp, err := http.Get(downloadURL)
	if err != nil {
		u.setError(err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		err = fmt.Errorf("下載失敗: HTTP %d", resp.StatusCode)
		u.setError(err)
		return err
	}

	out, err := os.Create(downloadPath)
	if err != nil {
		u.setError(err)
		return err
	}
	defer out.Close()

	totalSize := resp.ContentLength
	var downloaded int64
	buf := make([]byte, 32*1024)

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
			downloaded += int64(n)
			if totalSize > 0 {
				u.mu.Lock()
				u.downloadProgress = float64(downloaded) / float64(totalSize) * 100
				u.mu.Unlock()
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			u.setError(err)
			return err
		}
	}

	// 設定執行權限（Unix）
	if runtime.GOOS != "windows" {
		os.Chmod(downloadPath, 0755)
	}

	u.mu.Lock()
	u.downloadedPath = downloadPath
	u.downloadProgress = 100
	u.mu.Unlock()

	return nil
}

// ApplyUpdate 套用更新
func (u *Updater) ApplyUpdate() error {
	u.mu.RLock()
	downloadedPath := u.downloadedPath
	u.mu.RUnlock()

	if downloadedPath == "" {
		return fmt.Errorf("尚未下載更新")
	}

	config, err := GetInstallConfig()
	if err != nil {
		return err
	}

	// 複製新版本到安裝目錄
	if err := copyFile(downloadedPath, config.ExePath); err != nil {
		return fmt.Errorf("無法替換執行檔: %w", err)
	}

	// 設定執行權限
	if runtime.GOOS != "windows" {
		os.Chmod(config.ExePath, 0755)
	}

	// 清理下載的檔案
	os.Remove(downloadedPath)

	return nil
}

func (u *Updater) setError(err error) {
	u.mu.Lock()
	u.lastError = err
	u.mu.Unlock()
}

// normalizeVersion 正規化版本號
func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	return v
}

// compareVersions 比較版本號
func compareVersions(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")

	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}

	for i := 0; i < maxLen; i++ {
		var aNum, bNum int
		if i < len(aParts) {
			fmt.Sscanf(aParts[i], "%d", &aNum)
		}
		if i < len(bParts) {
			fmt.Sscanf(bParts[i], "%d", &bNum)
		}
		if aNum > bNum {
			return 1
		}
		if aNum < bNum {
			return -1
		}
	}
	return 0
}
