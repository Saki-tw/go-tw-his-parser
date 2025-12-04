// Package parser HIS 廠商解析器分配器
// 統一處理不同 HIS 廠商的檔案解析
package parser

import (
	"fmt"
	"io"
	"strings"
)

// HISVendor 支援的 HIS 廠商
type HISVendor string

const (
	VendorAuto     HISVendor = "auto"     // 自動偵測
	VendorNHI      HISVendor = "nhi"      // 健保署標準格式
	VendorYaosheng HISVendor = "yaosheng" // 耀聖
	VendorVision   HISVendor = "vision"   // 展望
	VendorDrMaster HISVendor = "drmaster" // 看診大師
	VendorGeneric  HISVendor = "generic"  // 通用格式
)

// VendorInfo 廠商資訊
type VendorInfo struct {
	Code        HISVendor `json:"code"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Formats     []string  `json:"formats"` // 支援的格式
}

// GetSupportedVendors 取得支援的廠商列表
func GetSupportedVendors() []VendorInfo {
	return []VendorInfo{
		{
			Code:        VendorAuto,
			Name:        "自動偵測",
			Description: "系統自動判斷檔案格式與來源",
			Formats:     []string{"xml", "csv", "txt", "dat"},
		},
		{
			Code:        VendorNHI,
			Name:        "健保署標準",
			Description: "健保署每日上傳 XML / 月申報 CSV",
			Formats:     []string{"xml", "csv"},
		},
		{
			Code:        VendorYaosheng,
			Name:        "耀聖 HIS",
			Description: "耀聖資訊 HIS 系統匯出檔案",
			Formats:     []string{"xml", "csv", "dat", "txt"},
		},
		{
			Code:        VendorVision,
			Name:        "展望 HIS",
			Description: "展望亞洲 HIS 系統匯出檔案",
			Formats:     []string{"xml", "csv"},
		},
		{
			Code:        VendorDrMaster,
			Name:        "看診大師",
			Description: "看診大師 HIS 系統匯出檔案",
			Formats:     []string{"xml", "csv", "txt"},
		},
		{
			Code:        VendorGeneric,
			Name:        "通用格式",
			Description: "標準 CSV 格式（自動欄位對應）",
			Formats:     []string{"csv", "txt"},
		},
	}
}

// ParseHISFileByVendor 根據指定廠商解析 HIS 檔案
func ParseHISFileByVendor(r io.Reader, filename string, vendor HISVendor) (*HISImportResult, error) {
	switch vendor {
	case VendorYaosheng:
		return ParseYaoshengFile(r, filename)

	case VendorVision:
		return ParseVisionFile(r, filename)

	case VendorDrMaster:
		return ParseDrMasterFile(r, filename)

	case VendorNHI:
		return ParseHISFile(r, filename) // 使用原有的健保署標準解析器

	case VendorGeneric:
		content, err := io.ReadAll(r)
		if err != nil {
			return nil, err
		}
		return parseGenericCSV(strings.NewReader(string(content)), detectBig5(content))

	case VendorAuto:
		fallthrough
	default:
		// 自動偵測
		return ParseHISFileAuto(r, filename)
	}
}

// ParseHISFileAuto 自動偵測廠商並解析
func ParseHISFileAuto(r io.Reader, filename string) (*HISImportResult, error) {
	content, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("讀取檔案失敗: %w", err)
	}

	// 偵測廠商
	vendor := detectVendor(content, filename)

	// 使用偵測到的廠商進行解析
	return ParseHISFileByVendor(strings.NewReader(string(content)), filename, vendor)
}

// detectVendor 偵測 HIS 廠商
func detectVendor(content []byte, filename string) HISVendor {
	contentStr := string(content)
	lowerFilename := strings.ToLower(filename)

	// 根據檔名判斷
	if strings.Contains(lowerFilename, "yaosheng") ||
	   strings.Contains(lowerFilename, "耀聖") ||
	   strings.Contains(lowerFilename, "ys_") {
		return VendorYaosheng
	}

	if strings.Contains(lowerFilename, "vision") ||
	   strings.Contains(lowerFilename, "展望") ||
	   strings.Contains(lowerFilename, "vs_") {
		return VendorVision
	}

	if strings.Contains(lowerFilename, "drmaster") ||
	   strings.Contains(lowerFilename, "看診大師") ||
	   strings.Contains(lowerFilename, "dm_") {
		return VendorDrMaster
	}

	// 根據內容特徵判斷
	// DAT 格式 (耀聖特有)
	if strings.HasSuffix(lowerFilename, ".dat") {
		return VendorYaosheng
	}

	// 看診大師使用 | 分隔符
	if strings.Contains(contentStr, "|") && !strings.Contains(contentStr, ",") {
		return VendorDrMaster
	}

	// XML 格式檢查
	if strings.Contains(contentStr, "<?xml") || strings.Contains(contentStr, "<RECS>") {
		// 檢查是否有廠商特有欄位
		if strings.Contains(contentStr, "<d23>") || strings.Contains(contentStr, "<d24>") {
			// d23=手機, d24=緊急聯絡人 為看診大師特有
			return VendorDrMaster
		}
		if strings.Contains(contentStr, "<d22>") {
			// d22=地址 為展望特有
			return VendorVision
		}
		// 預設使用健保署標準格式
		return VendorNHI
	}

	// CSV 格式
	if strings.Contains(contentStr, ",") {
		// 檢查是否為健保申報格式 (T/D/P 記錄類型)
		firstLine := strings.Split(contentStr, "\n")[0]
		firstChar := strings.TrimSpace(firstLine)
		if len(firstChar) > 0 {
			switch strings.ToUpper(string(firstChar[0])) {
			case "T":
				return VendorNHI
			}
		}

		// 檢查標題行特徵
		if strings.Contains(strings.ToLower(firstLine), "yaosheng") ||
		   strings.Contains(firstLine, "耀聖") {
			return VendorYaosheng
		}
		if strings.Contains(strings.ToLower(firstLine), "vision") ||
		   strings.Contains(firstLine, "展望") {
			return VendorVision
		}
		if strings.Contains(strings.ToLower(firstLine), "drmaster") ||
		   strings.Contains(firstLine, "看診大師") {
			return VendorDrMaster
		}
	}

	// 預設使用通用解析器
	return VendorGeneric
}

// GetVendorName 取得廠商中文名稱
func GetVendorName(vendor HISVendor) string {
	switch vendor {
	case VendorYaosheng:
		return "耀聖"
	case VendorVision:
		return "展望"
	case VendorDrMaster:
		return "看診大師"
	case VendorNHI:
		return "健保署標準"
	case VendorGeneric:
		return "通用格式"
	case VendorAuto:
		return "自動偵測"
	default:
		return string(vendor)
	}
}
