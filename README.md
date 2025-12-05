# go-tw-his-parser

> **A high-performance Golang parser for Taiwan NHI (National Health Insurance) VPN declaration format. Handles legacy HIS data, IC card uploads, and prescription decoding.**
>
> **台灣健保申報格式解析器** (健保醫療資訊系統/醫令清單/健保卡上傳格式)

🌐 **網站**：https://saki-tw.github.io/go-tw-his-parser/

[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8.svg)](https://go.dev/)
[![Saki Studio](https://img.shields.io/badge/Maintained%20by-Saki%20Studio-c6a4cf)](https://saki-studio.com.tw)

**Golang HIS 資料解析器 — 台灣健保申報格式解析的開源解決方案**

這是一個專為處理台灣常見醫療軟體（如耀聖、展望、看診大師等）匯出資料所設計的 Go 語言解析庫。它旨在解決健保申報格式封閉、HIS 資料混亂且缺乏標準化的痛點，提供開發者一個乾淨、強型別的統一介面，支援健保 IC 卡上傳格式與 VPN 申報檔解析。

---

## 支援的 HIS 廠商

| 廠商 | 支援格式 | 說明 |
|------|----------|------|
| 健保署標準 | XML, CSV | 每日上傳檔、月申報檔 |
| 耀聖 HIS | XML, CSV, DAT, TXT | 完整支援各種匯出格式 |
| 展望 HIS | XML, CSV | 常見的診所系統 |
| 看診大師 | XML, CSV, TXT | 支援 pipe 分隔格式 |
| 通用格式 | CSV, TXT | 標準逗號分隔檔案 |

---

## 安裝

```bash
go get github.com/Saki-tw/go-tw-his-parser
```

---

## 下載與使用

### 步驟一：下載程式

前往 [Releases 頁面](https://github.com/Saki-tw/go-tw-his-parser/releases) 下載：

| 作業系統 | 下載檔案 |
|----------|----------|
| Windows | `his-parser-web-windows.exe` |
| macOS (Apple Silicon) | `his-parser-web-darwin-arm64` |
| Linux | `his-parser-web-linux-amd64` |

#### 安全性說明

我們理解下載不認識的執行檔會有疑慮，因此提供以下驗證方式：

| 驗證方式 | 說明 |
|----------|------|
| **GitHub Actions 自動建置** | 執行檔在 GitHub 伺服器編譯，[建置過程完全公開](../../actions) |
| **SHA256 校驗碼** | 每個版本附 `checksums.txt`，可驗證檔案未被竄改 |
| **原始碼公開** | 100% 開源，歡迎檢視每一行程式碼 |

<details>
<summary>如何驗證 SHA256 校驗碼？</summary>

**Windows (PowerShell)**
```powershell
Get-FileHash -Algorithm SHA256 his-parser-web-windows.exe
```

**macOS / Linux**
```bash
sha256sum his-parser-web-darwin-arm64
```

將結果與 `checksums.txt` 內的值比對，相同即表示檔案完整。

</details>

### 步驟二：一鍵安裝

**雙擊執行檔**，程式會：
1. **首次執行**：自動安裝到使用者目錄並建立捷徑
2. 在本機啟動伺服器
3. 自動開啟瀏覽器

| 平台 | 安裝位置 | 捷徑 |
|------|----------|------|
| Windows | `%LOCALAPPDATA%\HIS Parser\` | 開始選單 + 桌面 |
| macOS | `~/Applications/HIS Parser.app` | 啟動台 |
| Linux | `~/.local/share/his-parser/` | 應用程式選單 |

> macOS 首次執行可能需要：系統設定 → 隱私與安全性 → 仍要打開
>
> Windows 無需管理員權限，不會彈出 UAC 對話框

### 步驟三：上傳檔案解析

1. **選擇廠商**（不確定就選「自動偵測」）
2. **點擊「選擇檔案」** 或拖放檔案
3. **查看結果**：病患列表、處方列表、詳細輸出
4. **匯出資料**：JSON 或 CSV 格式

支援格式：`.xml`、`.csv`、`.txt`、`.dat`

### 步驟四：關閉程式

關閉終端機視窗，或按 `Ctrl+C`。

---

## 隱私保護

此程式：

- **本機解析**：所有資料解析都在您的電腦上進行
- **不儲存資料**：解析完成後資料僅存在記憶體中
- **不上傳資料**：醫療資料不會傳輸至任何外部伺服器
- **遮蔽顯示**：身分證號碼自動遮蔽（例如：A12****789）
- **自動更新**：僅連接 GitHub API 檢查新版本，不傳送任何使用者資料

---

## 功能特色

### 一鍵安裝與自動更新

首次執行自動完成安裝，無需額外設定。程式會在背景檢查 GitHub Releases，有新版本時在介面中提示更新。

### 自動編碼偵測

台灣的 HIS 系統歷史悠久，很多還在使用 Big5 編碼。本解析器會自動偵測檔案編碼，無論是 Big5 還是 UTF-8 都能正確處理，不會出現亂碼。

### 民國年轉換

健保相關檔案大量使用民國年格式（如 1140315 代表民國 114 年 3 月 15 日），解析器會自動轉換成標準格式（2025-03-15）。

### 慢箋識別

解析器能夠根據就醫類別、IC 序號、給藥天數等資訊，自動判斷處方是否為慢性病連續處方箋，並記錄這是第幾次領藥。

### 廠商特徵辨識

即使檔名沒有明確標示來源，解析器也能根據檔案內容的特徵自動判斷是哪家廠商的格式。

---

## 開發者資訊

<details>
<summary>從原始碼執行（需安裝 Go）</summary>

### 安裝 Go

前往 https://go.dev/dl/ 下載安裝

### 執行

```bash
go run github.com/Saki-tw/go-tw-his-parser/cmd/web@latest
```

或 clone 後執行：

```bash
git clone https://github.com/Saki-tw/go-tw-his-parser.git
cd go-tw-his-parser/cmd/web
go run main.go
```

</details>

<details>
<summary>作為程式庫使用</summary>

```bash
go get github.com/Saki-tw/go-tw-his-parser
```

```go
package main

import (
    "fmt"
    "os"

    parser "github.com/Saki-tw/go-tw-his-parser"
)

func main() {
    f, _ := os.Open("病患資料.xml")
    defer f.Close()

    result, _ := parser.ParseHISFileByVendor(f, "病患資料.xml", parser.VendorAuto)

    fmt.Printf("解析完成：%d 位病患、%d 筆處方\n",
        len(result.Patients),
        len(result.Prescriptions))
}
```

</details>

<details>
<summary>自行編譯執行檔</summary>

```bash
cd cmd/web

# Windows
GOOS=windows GOARCH=amd64 go build -o his-parser-web.exe

# macOS (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o his-parser-web-darwin-arm64

# Linux
GOOS=linux GOARCH=amd64 go build -o his-parser-web-linux-amd64
```

</details>

---

## 開發緣起

本專案脫胎自 [Saki Pharmacy OS](https://saki-studio.com.tw/saki-pharmacy-os/) 內部使用的資料處理模組。

在開發過程中，我們發現這部分的工作其實就是不斷地蒐集各家 HIS 廠商的格式規格、撰寫對應的解析程式。這些程式碼本身並不涉及商業邏輯，純粹是格式轉換的苦工。既然如此，不如直接開源出來，讓其他有相同需求的開發者省去重複造輪子的麻煩。

---

## 授權

本專案以 MIT 授權釋出。

---

## 格式資訊

如果您有其他 HIS 廠商的匯出檔案格式規格，或是現有支援廠商的新格式，歡迎提供。

---

## ⚠️ Help Wanted

此專案目前基於健保署規格書開發，尚未在真實硬體環境大規模測試。如果你手邊有讀卡機或掃描槍、或根本就是藥局，歡迎提供回報測試結果，E-mail 或臉書都能聯絡到我！

---

## 致謝

感謝**妙法無邊雷射蓮花宗**在艱困時刻寄來的糖與米。

---

## 供養 / Support

如果這個工具幫到你，可以請我活下去：

👉 [Touch me if you had desolation](https://saki-tw.github.io/-Touch-me-if-you-had-desolation/)

---

© 2025 [Saki Studio](https://saki-studio.com.tw/)
