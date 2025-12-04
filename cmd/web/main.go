// Package main 台灣醫療資料解析器 - 本地 Web 版
// 雙擊執行後自動安裝、瀏覽器自動開啟
package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	parser "github.com/Saki-tw/go-tw-his-parser"
)

//go:embed index.html
var indexHTML embed.FS

// 全域更新管理器
var updater *Updater

func main() {
	// 一鍵安裝：首次執行時自動安裝到使用者目錄
	if !CheckAndInstall() {
		// 已啟動新安裝的版本，結束目前程式
		os.Exit(0)
	}

	// 初始化更新管理器並啟動背景檢查
	updater = NewUpdater(AppVersion)
	updater.Start()

	// 找到可用的埠
	port := findAvailablePort()
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	url := fmt.Sprintf("http://%s", addr)

	// 設定路由
	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/api/parse", handleParse)
	http.HandleFunc("/api/vendors", handleVendors)

	// 更新 API
	http.HandleFunc("/api/update/status", handleUpdateStatus)
	http.HandleFunc("/api/update/check", handleUpdateCheck)
	http.HandleFunc("/api/update/download", handleUpdateDownload)
	http.HandleFunc("/api/update/apply", handleUpdateApply)

	// 啟動伺服器（非阻塞）
	server := &http.Server{Addr: addr}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("伺服器錯誤: %v\n", err)
		}
	}()

	// 等待伺服器啟動
	time.Sleep(100 * time.Millisecond)

	// 自動開啟瀏覽器
	fmt.Printf("台灣醫療資料解析器 v%s 已啟動\n", AppVersion)
	fmt.Printf("請在瀏覽器開啟: %s\n", url)
	fmt.Printf("按 Ctrl+C 關閉程式\n\n")
	openBrowser(url)

	// 保持運行
	select {}
}

// findAvailablePort 找到可用的埠
func findAvailablePort() int {
	// 嘗試常用埠
	ports := []int{8080, 8081, 8082, 3000, 3001, 5000}
	for _, port := range ports {
		if isPortAvailable(port) {
			return port
		}
	}
	// 讓系統分配
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 8080
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func isPortAvailable(port int) bool {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	listener.Close()
	return true
}

// openBrowser 開啟預設瀏覽器
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default: // Linux
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}

// handleIndex 首頁
func handleIndex(w http.ResponseWriter, r *http.Request) {
	data, _ := indexHTML.ReadFile("index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

// handleVendors 取得廠商列表
func handleVendors(w http.ResponseWriter, r *http.Request) {
	vendors := parser.GetSupportedVendors()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(vendors)
}

// handleParse 解析檔案
func handleParse(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 限制上傳大小 50MB
	r.ParseMultipartForm(50 << 20)

	file, header, err := r.FormFile("file")
	if err != nil {
		sendError(w, "無法讀取檔案: "+err.Error())
		return
	}
	defer file.Close()

	// 取得廠商選擇
	vendorStr := r.FormValue("vendor")
	vendor := parser.HISVendor(vendorStr)
	if vendor == "" {
		vendor = parser.VendorAuto
	}

	// 讀取檔案內容
	content, err := io.ReadAll(file)
	if err != nil {
		sendError(w, "讀取檔案失敗: "+err.Error())
		return
	}

	// 解析
	result, err := parser.ParseHISFileByVendor(
		&byteReader{data: content, pos: 0},
		header.Filename,
		vendor,
	)
	if err != nil {
		sendError(w, "解析失敗: "+err.Error())
		return
	}

	// 遮蔽身分證
	for i := range result.Patients {
		result.Patients[i].NationalID = maskID(result.Patients[i].NationalID)
	}
	for i := range result.Prescriptions {
		result.Prescriptions[i].PatientID = maskID(result.Prescriptions[i].PatientID)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func sendError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"errors":  []string{msg},
	})
}

// maskID 遮蔽身分證
func maskID(id string) string {
	if len(id) < 4 {
		return id
	}
	runes := []rune(id)
	if len(runes) >= 10 {
		return string(runes[:3]) + "****" + string(runes[7:])
	}
	return string(runes[:2]) + "****"
}

// byteReader 實作 io.Reader
type byteReader struct {
	data []byte
	pos  int
}

func (r *byteReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// =====================================================================
// 更新 API Handlers
// =====================================================================

// handleUpdateStatus 取得更新狀態
func handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	status := updater.GetStatus()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handleUpdateCheck 手動觸發更新檢查
func handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	go updater.CheckForUpdate()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "已開始檢查更新",
	})
}

// handleUpdateDownload 下載更新
func handleUpdateDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !updater.IsUpdateAvailable() {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "沒有可用的更新",
		})
		return
	}

	go updater.DownloadUpdate()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "已開始下載更新",
	})
}

// handleUpdateApply 套用更新
func handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := updater.GetStatus()
	if !status.DownloadReady {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "請先下載更新",
		})
		return
	}

	// 先回應成功
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "正在套用更新，請重新啟動程式",
	})

	// 延遲套用更新
	go func() {
		time.Sleep(500 * time.Millisecond)
		if err := updater.ApplyUpdate(); err != nil {
			fmt.Printf("套用更新失敗: %v\n", err)
		} else {
			fmt.Println("更新已套用，請重新啟動程式")
		}
	}()
}
