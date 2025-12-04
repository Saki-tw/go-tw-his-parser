// Package parser 耀聖 HIS 資料格式解析器
// 耀聖為台灣市佔率前三的藥局 HIS 系統
package parser

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

// ============================================================================
// 耀聖 HIS 專屬格式定義
// ============================================================================

// YaoshengExportType 耀聖匯出類型
type YaoshengExportType string

const (
	YaoshengXML   YaoshengExportType = "xml"   // 健保每日上傳 XML
	YaoshengCSV   YaoshengExportType = "csv"   // 月申報/報表匯出 CSV
	YaoshengDAT   YaoshengExportType = "dat"   // 耀聖專用 DAT 格式
	YaoshengTXT   YaoshengExportType = "txt"   // 文字報表格式
)

// YaoshengXMLRoot 耀聖 XML 根元素 (基於健保署格式，但欄位順序略有不同)
type YaoshengXMLRoot struct {
	XMLName xml.Name        `xml:"RECS"`
	Records []YaoshengRec   `xml:"REC"`
}

// YaoshengRec 耀聖單筆記錄
type YaoshengRec struct {
	// 表頭
	HospitalCode  string `xml:"h1"`  // 醫事機構代號
	FeeYearMonth  string `xml:"h2"`  // 費用年月 (民國 YYYMM)
	ClaimType     string `xml:"h3"`  // 申報類別

	// 病患資料
	DataFormat    string `xml:"A01"` // 資料格式
	CardNo        string `xml:"A11"` // 健保卡號
	NationalID    string `xml:"A12"` // 身分證
	Birthday      string `xml:"A13"` // 生日 (民國 YYYMMDD)
	SourceHosp    string `xml:"A14"` // 原處方醫院
	VisitDateTime string `xml:"A17"` // 就診日期時間
	VisitSeq      string `xml:"A18"` // 就醫序號
	VisitType     string `xml:"A23"` // 就醫類別

	// 診斷與病患資訊
	DiagCode      string `xml:"d19"` // 診斷碼
	PatientName   string `xml:"d20"` // 病患姓名
	PatientPhone  string `xml:"d21"` // 病患電話
	PharmacistID  string `xml:"d31"` // 藥師身分證
	PharmacistName string `xml:"d32"` // 藥師姓名

	// 藥品明細 (耀聖格式會內嵌多筆)
	Items []YaoshengItem `xml:"MB2"`
}

// YaoshengItem 耀聖藥品項目
type YaoshengItem struct {
	OrderType  string `xml:"p1"`  // 醫令類別
	DrugCode   string `xml:"p2"`  // 藥品代碼
	DrugName   string `xml:"p3"`  // 藥品名稱
	Frequency  string `xml:"p5"`  // 使用頻率
	Route      string `xml:"p6"`  // 給藥途徑
	Quantity   string `xml:"p7"`  // 總量
	UnitPrice  string `xml:"p8"`  // 單價
	DaysSupply string `xml:"d27"` // 給藥天數
	RefillNo   string `xml:"d36"` // 慢箋次數
}

// YaoshengDATRecord 耀聖 DAT 格式記錄 (固定欄位寬度)
type YaoshengDATRecord struct {
	RecordType   string // 1=表頭, 2=明細, 9=表尾
	HospitalCode string // 醫院代碼 (10 碼)
	NationalID   string // 身分證 (10 碼)
	PatientName  string // 姓名 (20 碼)
	Birthday     string // 生日 (7 碼，民國)
	VisitDate    string // 就診日期 (7 碼)
	DrugCode     string // 藥品代碼 (10 碼)
	DrugName     string // 藥品名稱 (40 碼)
	Quantity     string // 數量 (10 碼)
	Days         string // 天數 (3 碼)
}

// ============================================================================
// 耀聖解析器
// ============================================================================

// ParseYaoshengFile 解析耀聖 HIS 匯出檔案
func ParseYaoshengFile(r io.Reader, filename string) (*HISImportResult, error) {
	content, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("讀取檔案失敗: %w", err)
	}

	// 偵測編碼並轉換
	isBig5 := detectBig5(content)
	var contentStr string
	if isBig5 {
		decoded, _, err := transform.Bytes(traditionalchinese.Big5.NewDecoder(), content)
		if err == nil {
			contentStr = string(decoded)
		} else {
			contentStr = string(content)
		}
	} else {
		contentStr = string(content)
	}

	// 判斷格式
	lowerFilename := strings.ToLower(filename)

	// XML 格式
	if strings.HasSuffix(lowerFilename, ".xml") ||
	   strings.Contains(contentStr, "<?xml") ||
	   strings.Contains(contentStr, "<RECS>") {
		return parseYaoshengXML(contentStr)
	}

	// DAT 格式 (固定寬度)
	if strings.HasSuffix(lowerFilename, ".dat") {
		return parseYaoshengDAT(contentStr)
	}

	// CSV/TXT 格式
	return parseYaoshengCSV(contentStr)
}

// parseYaoshengXML 解析耀聖 XML 格式
func parseYaoshengXML(content string) (*HISImportResult, error) {
	result := &HISImportResult{
		SourceType:   "xml",
		SourceVendor: "yaosheng",
	}

	var xmlData YaoshengXMLRoot
	if err := xml.Unmarshal([]byte(content), &xmlData); err != nil {
		result.Errors = append(result.Errors, "XML 解析失敗: "+err.Error())
		return result, err
	}

	result.Total = len(xmlData.Records)
	patientMap := make(map[string]*HISPatient)

	for i, rec := range xmlData.Records {
		// 提取病患
		if rec.NationalID != "" {
			patient := &HISPatient{
				NationalID: strings.TrimSpace(rec.NationalID),
				Name:       strings.TrimSpace(rec.PatientName),
				CardNumber: strings.TrimSpace(rec.CardNo),
				Phone:      strings.TrimSpace(rec.PatientPhone),
			}
			if rec.Birthday != "" && len(rec.Birthday) >= 7 {
				patient.Birthday = convertROCDate(rec.Birthday[:7])
			}
			if _, exists := patientMap[patient.NationalID]; !exists {
				patientMap[patient.NationalID] = patient
			}
		}

		// 提取處方
		rx := &HISPrescription{
			PatientID:      strings.TrimSpace(rec.NationalID),
			ProviderCode:   strings.TrimSpace(rec.SourceHosp),
			VisitType:      strings.TrimSpace(rec.VisitType),
			VisitSequence:  strings.TrimSpace(rec.VisitSeq),
			DiagnosisCode:  strings.TrimSpace(rec.DiagCode),
			PharmacistID:   strings.TrimSpace(rec.PharmacistID),
			PharmacistName: strings.TrimSpace(rec.PharmacistName),
			DataFormat:     strings.TrimSpace(rec.DataFormat),
		}

		// 解析就診日期時間
		if rec.VisitDateTime != "" && len(rec.VisitDateTime) >= 7 {
			rx.DispenseDate = convertROCDate(rec.VisitDateTime[:7])
			if len(rec.VisitDateTime) >= 13 {
				rx.DispenseTime = rec.VisitDateTime[7:9] + ":" + rec.VisitDateTime[9:11] + ":" + rec.VisitDateTime[11:13]
			}
		}

		// 生成處方序號
		rx.PrescriptionNo = fmt.Sprintf("YS-%s-%s-%s", rx.ProviderCode, rx.DispenseDate, rx.VisitSequence)

		// 解析慢箋次數
		if strings.HasPrefix(rx.VisitSequence, "IC") && len(rx.VisitSequence) >= 4 {
			if n, err := strconv.Atoi(rx.VisitSequence[2:4]); err == nil {
				rx.ChronicRefillNo = n
			}
		}

		// 解析藥品項目
		for _, item := range rec.Items {
			rxItem := HISPrescriptionItem{
				OrderType: strings.TrimSpace(item.OrderType),
				DrugCode:  strings.TrimSpace(item.DrugCode),
				DrugName:  strings.TrimSpace(item.DrugName),
				Frequency: strings.TrimSpace(item.Frequency),
				Route:     strings.TrimSpace(item.Route),
			}
			if item.Quantity != "" {
				rxItem.Quantity, _ = strconv.ParseFloat(strings.TrimSpace(item.Quantity), 64)
			}
			if item.UnitPrice != "" {
				rxItem.UnitPrice, _ = strconv.ParseFloat(strings.TrimSpace(item.UnitPrice), 64)
			}
			if item.DaysSupply != "" {
				rxItem.DaysSupply, _ = strconv.Atoi(strings.TrimSpace(item.DaysSupply))
			}
			rx.Items = append(rx.Items, rxItem)
		}

		if len(rx.Items) > 0 || rx.PatientID != "" {
			result.Prescriptions = append(result.Prescriptions, *rx)
			result.Imported++
		} else {
			result.Errors = append(result.Errors, fmt.Sprintf("第 %d 筆記錄無有效資料", i+1))
			result.Failed++
		}
	}

	for _, p := range patientMap {
		result.Patients = append(result.Patients, *p)
	}

	result.Success = result.Failed == 0
	return result, nil
}

// parseYaoshengDAT 解析耀聖 DAT 格式 (固定欄位寬度)
func parseYaoshengDAT(content string) (*HISImportResult, error) {
	result := &HISImportResult{
		SourceType:   "dat",
		SourceVendor: "yaosheng",
	}

	scanner := bufio.NewScanner(strings.NewReader(content))
	patientMap := make(map[string]*HISPatient)
	rxMap := make(map[string]*HISPrescription)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if len(line) < 10 {
			continue
		}

		// 耀聖 DAT 格式: 固定欄位寬度
		// 位置 0-0: 記錄類型 (1=表頭, 2=明細, 9=表尾)
		// 位置 1-10: 醫院代碼
		// 位置 11-20: 身分證
		// 位置 21-40: 姓名
		// 位置 41-47: 生日
		// 位置 48-54: 就診日期
		// 位置 55-64: 藥品代碼
		// 位置 65-104: 藥品名稱
		// 位置 105-114: 數量
		// 位置 115-117: 天數

		recordType := string(line[0])

		if recordType == "2" { // 明細記錄
			result.Total++

			nationalID := strings.TrimSpace(safeSubstring(line, 11, 21))
			name := strings.TrimSpace(safeSubstring(line, 21, 41))
			birthday := strings.TrimSpace(safeSubstring(line, 41, 48))
			visitDate := strings.TrimSpace(safeSubstring(line, 48, 55))
			drugCode := strings.TrimSpace(safeSubstring(line, 55, 65))
			drugName := strings.TrimSpace(safeSubstring(line, 65, 105))
			qtyStr := strings.TrimSpace(safeSubstring(line, 105, 115))
			daysStr := strings.TrimSpace(safeSubstring(line, 115, 118))

			// 建立病患
			if nationalID != "" {
				if _, exists := patientMap[nationalID]; !exists {
					patient := &HISPatient{
						NationalID: nationalID,
						Name:       name,
					}
					if len(birthday) >= 7 {
						patient.Birthday = convertROCDate(birthday)
					}
					patientMap[nationalID] = patient
				}
			}

			// 建立處方
			rxKey := nationalID + "-" + visitDate
			if _, exists := rxMap[rxKey]; !exists {
				dispenseDate := ""
				if len(visitDate) >= 7 {
					dispenseDate = convertROCDate(visitDate)
				}
				rxMap[rxKey] = &HISPrescription{
					PatientID:      nationalID,
					PrescriptionNo: fmt.Sprintf("YS-%s-%s", nationalID, visitDate),
					DispenseDate:   dispenseDate,
				}
			}

			// 加入藥品項目
			if drugCode != "" {
				qty, _ := strconv.ParseFloat(qtyStr, 64)
				days, _ := strconv.Atoi(daysStr)
				rxMap[rxKey].Items = append(rxMap[rxKey].Items, HISPrescriptionItem{
					OrderType:  "1",
					DrugCode:   drugCode,
					DrugName:   drugName,
					Quantity:   qty,
					DaysSupply: days,
				})
			}

			result.Imported++
		}
	}

	for _, p := range patientMap {
		result.Patients = append(result.Patients, *p)
	}
	for _, rx := range rxMap {
		result.Prescriptions = append(result.Prescriptions, *rx)
	}

	result.Success = result.Failed == 0
	return result, nil
}

// parseYaoshengCSV 解析耀聖 CSV 格式
func parseYaoshengCSV(content string) (*HISImportResult, error) {
	result := &HISImportResult{
		SourceType:   "csv",
		SourceVendor: "yaosheng",
	}

	scanner := bufio.NewScanner(strings.NewReader(content))
	patientMap := make(map[string]*HISPatient)
	rxMap := make(map[string]*HISPrescription)
	lineNum := 0
	var headers []string
	colMap := make(map[string]int)

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		fields := parseCSVLine(line)

		// 第一行可能是標題
		if lineNum == 1 {
			// 檢查是否為標題行
			if isYaoshengHeaderLine(fields) {
				headers = fields
				colMap = buildYaoshengColumnMapping(headers)
				continue
			}
			// 不是標題，使用預設欄位順序
			colMap = getYaoshengDefaultColumns()
		}

		result.Total++

		// 提取資料
		nationalID := getFieldByKey(fields, colMap, "national_id")
		name := getFieldByKey(fields, colMap, "name")
		birthday := getFieldByKey(fields, colMap, "birthday")
		visitDate := getFieldByKey(fields, colMap, "visit_date")
		drugCode := getFieldByKey(fields, colMap, "drug_code")
		drugName := getFieldByKey(fields, colMap, "drug_name")
		qtyStr := getFieldByKey(fields, colMap, "quantity")
		daysStr := getFieldByKey(fields, colMap, "days")
		visitType := getFieldByKey(fields, colMap, "visit_type")

		// 建立病患
		if nationalID != "" {
			if _, exists := patientMap[nationalID]; !exists {
				patient := &HISPatient{
					NationalID: nationalID,
					Name:       name,
				}
				if len(birthday) >= 7 {
					patient.Birthday = convertROCDate(birthday)
				} else if birthday != "" {
					patient.Birthday = birthday
				}
				patientMap[nationalID] = patient
			}
		}

		// 建立處方
		if nationalID != "" && visitDate != "" {
			rxKey := nationalID + "-" + visitDate
			if _, exists := rxMap[rxKey]; !exists {
				dispenseDate := visitDate
				if len(visitDate) == 7 {
					dispenseDate = convertROCDate(visitDate)
				}
				rxMap[rxKey] = &HISPrescription{
					PatientID:      nationalID,
					PrescriptionNo: fmt.Sprintf("YS-%s-%s", nationalID, visitDate),
					DispenseDate:   dispenseDate,
					VisitType:      visitType,
				}

				// 判斷慢箋
				if visitType == "08" {
					rxMap[rxKey].ChronicRefillNo = 1
				}
			}

			// 加入藥品項目
			if drugCode != "" {
				qty, _ := strconv.ParseFloat(qtyStr, 64)
				days, _ := strconv.Atoi(daysStr)
				rxMap[rxKey].Items = append(rxMap[rxKey].Items, HISPrescriptionItem{
					OrderType:  "1",
					DrugCode:   drugCode,
					DrugName:   drugName,
					Quantity:   qty,
					DaysSupply: days,
				})

				// 若天數 >= 28，視為慢箋
				if days >= 28 && rxMap[rxKey].ChronicRefillNo == 0 {
					rxMap[rxKey].ChronicRefillNo = 1
				}
			}
		}

		result.Imported++
	}

	for _, p := range patientMap {
		result.Patients = append(result.Patients, *p)
	}
	for _, rx := range rxMap {
		result.Prescriptions = append(result.Prescriptions, *rx)
	}

	result.Success = result.Failed == 0
	return result, nil
}

// ============================================================================
// 輔助函數
// ============================================================================

// isYaoshengHeaderLine 判斷是否為耀聖 CSV 標題行
func isYaoshengHeaderLine(fields []string) bool {
	if len(fields) < 3 {
		return false
	}

	headerKeywords := []string{"身分證", "姓名", "藥品", "日期", "代碼", "ID", "name", "drug"}
	matchCount := 0

	for _, f := range fields {
		f = strings.ToLower(f)
		for _, kw := range headerKeywords {
			if strings.Contains(f, strings.ToLower(kw)) {
				matchCount++
				break
			}
		}
	}

	return matchCount >= 2
}

// buildYaoshengColumnMapping 建立耀聖欄位對應
func buildYaoshengColumnMapping(headers []string) map[string]int {
	colMap := make(map[string]int)

	patterns := map[string][]string{
		"national_id": {"身分證", "身份證", "ID", "pid", "idno"},
		"name":        {"姓名", "name", "patient"},
		"birthday":    {"生日", "出生", "birthday", "dob"},
		"visit_date":  {"就診日", "就診日期", "調劑日", "日期", "date"},
		"drug_code":   {"藥品代碼", "藥碼", "健保碼", "code"},
		"drug_name":   {"藥品名稱", "藥名", "drug"},
		"quantity":    {"數量", "總量", "qty", "quantity"},
		"days":        {"天數", "日份", "days"},
		"visit_type":  {"就醫類別", "案件", "type"},
	}

	for i, h := range headers {
		h = strings.ToLower(strings.TrimSpace(h))
		for key, variants := range patterns {
			for _, v := range variants {
				if strings.Contains(h, strings.ToLower(v)) {
					colMap[key] = i
					break
				}
			}
		}
	}

	return colMap
}

// getYaoshengDefaultColumns 取得耀聖預設欄位順序
func getYaoshengDefaultColumns() map[string]int {
	// 耀聖常見匯出順序: 身分證, 姓名, 生日, 就診日, 藥品代碼, 藥品名稱, 數量, 天數, 就醫類別
	return map[string]int{
		"national_id": 0,
		"name":        1,
		"birthday":    2,
		"visit_date":  3,
		"drug_code":   4,
		"drug_name":   5,
		"quantity":    6,
		"days":        7,
		"visit_type":  8,
	}
}

// getFieldByKey 透過 key 取得欄位值
func getFieldByKey(fields []string, colMap map[string]int, key string) string {
	if idx, ok := colMap[key]; ok && idx < len(fields) {
		return strings.TrimSpace(fields[idx])
	}
	return ""
}

// safeSubstring 安全取子字串
func safeSubstring(s string, start, end int) string {
	if start < 0 {
		start = 0
	}
	if end > len(s) {
		end = len(s)
	}
	if start >= end {
		return ""
	}
	return s[start:end]
}
