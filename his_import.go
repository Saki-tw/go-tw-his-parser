// Package parser HIS 廠商資料匯入解析器
// 支援健保署每日上傳 XML 與費用申報 CSV 格式
package parser

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

// ============================================================================
// 健保署每日上傳 XML 格式結構 (Big5 編碼)
// ============================================================================

// NHIUploadXML 健保每日上傳 XML 根元素
type NHIUploadXML struct {
	XMLName xml.Name     `xml:"RECS"`
	Records []NHIRecord  `xml:"REC"`
}

// NHIRecord 單筆就醫/調劑紀錄
type NHIRecord struct {
	MSH  NHIMSH   `xml:"MSH"`  // 訊息表頭
	MB1  NHIMB1   `xml:"MB1"`  // 就醫基本資料
	MB2s []NHIMB2 `xml:"MB2"`  // 醫令明細 (多筆)
}

// NHIMSH 訊息表頭區段
type NHIMSH struct {
	H1  string `xml:"h1"`  // 醫事機構代號
	H2  string `xml:"h2"`  // 費用年月 (民國 YYYMM)
	H3  string `xml:"h3"`  // 申報類別
}

// NHIMB1 就醫基本資料區段
type NHIMB1 struct {
	A01 string `xml:"A01"` // 資料格式: 1=正常, 2=異常, 3=補正正常, 4=補正異常
	A11 string `xml:"A11"` // 卡片號碼
	A12 string `xml:"A12"` // 身分證號 (病患主鍵)
	A13 string `xml:"A13"` // 出生日期 (民國 YYYMMDD)
	A14 string `xml:"A14"` // 原處方醫療機構代碼
	A17 string `xml:"A17"` // 就診日期時間 (民國 YYYMMDDHHMMSS)
	A18 string `xml:"A18"` // 就醫序號 (IC02=慢箋第2次, IC03=第3次...)
	A23 string `xml:"A23"` // 就醫類別 (08=慢箋, AF=釋出處方)
	D19 string `xml:"d19"` // 主診斷代碼 (ICD-10)
	D20 string `xml:"d20"` // 病患姓名
	D21 string `xml:"d21"` // 病患電話
	D31 string `xml:"d31"` // 調劑藥師身分證
	D32 string `xml:"d32"` // 藥師姓名
}

// NHIMB2 醫令明細區段
type NHIMB2 struct {
	P1  string `xml:"p1"`  // 醫令類別: 1=藥品, 2=診療, 9=藥事服務費
	P2  string `xml:"p2"`  // 醫令代碼 (健保碼)
	P3  string `xml:"p3"`  // 藥品名稱
	P5  string `xml:"p5"`  // 使用頻率 (BID, TID, QID...)
	P6  string `xml:"p6"`  // 給藥途徑 (PO, EXT...)
	P7  string `xml:"p7"`  // 總量
	P8  string `xml:"p8"`  // 單價
	D27 string `xml:"d27"` // 給藥日份
	D36 string `xml:"d36"` // 連處次數 (慢箋第幾次)
}

// ============================================================================
// 健保署費用申報 CSV 格式結構
// ============================================================================

// NHIClaimCSV 費用申報 CSV 解析結果
type NHIClaimCSV struct {
	Header    NHIClaimHeader
	Claims    []NHIClaimDetail
	Items     []NHIClaimItem
}

// NHIClaimHeader 申報表頭
type NHIClaimHeader struct {
	T1  string // 資料格式 (30=藥局)
	T2  string // 服務機構代號
	T3  string // 費用年月
	T4  string // 申報類別
}

// NHIClaimDetail 門診費用明細
type NHIClaimDetail struct {
	D1  string  // 案件分類
	D2  string  // 流水號
	D3  string  // 就醫日期
	D4  string  // 病患身分證
	D5  string  // 病患姓名
	D39 float64 // 合計點數
	D40 float64 // 部分負擔
}

// NHIClaimItem 醫令項目
type NHIClaimItem struct {
	P1 string  // 醫令類別
	P2 string  // 醫令代碼
	P3 string  // 藥品名稱
	P7 float64 // 總量
	P8 float64 // 單價
}

// ============================================================================
// 轉換後的標準化資料結構
// ============================================================================

// HISImportResult HIS 匯入結果
type HISImportResult struct {
	Success       bool                `json:"success"`
	SourceType    string              `json:"source_type"`    // xml, csv
	SourceVendor  string              `json:"source_vendor"`  // nhi, yaosheng, vision, jubo
	Total         int                 `json:"total"`
	Imported      int                 `json:"imported"`
	Skipped       int                 `json:"skipped"`
	Failed        int                 `json:"failed"`
	Errors        []string            `json:"errors,omitempty"`
	Patients      []HISPatient        `json:"patients,omitempty"`
	Prescriptions []HISPrescription   `json:"prescriptions,omitempty"`
	DrugUsages    []HISDrugUsage      `json:"drug_usages,omitempty"`
}

// HISPatient 標準化病患資料
type HISPatient struct {
	NationalID   string  `json:"national_id"`
	Name         string  `json:"name"`
	Birthday     string  `json:"birthday,omitempty"`     // YYYY-MM-DD 格式
	Phone        string  `json:"phone,omitempty"`
	CardNumber   string  `json:"card_number,omitempty"`  // 健保卡號
}

// HISPrescription 標準化處方資料
type HISPrescription struct {
	PatientID        string           `json:"patient_id"`         // 身分證
	PrescriptionNo   string           `json:"prescription_no"`    // 處方序號
	DispenseDate     string           `json:"dispense_date"`      // 調劑日期 YYYY-MM-DD
	DispenseTime     string           `json:"dispense_time"`      // 調劑時間 HH:MM:SS
	VisitType        string           `json:"visit_type"`         // 就醫類別
	VisitSequence    string           `json:"visit_sequence"`     // 就醫序號 (IC01, IC02...)
	ChronicRefillNo  int              `json:"chronic_refill_no"`  // 慢箋第幾次
	ProviderCode     string           `json:"provider_code"`      // 原處方醫院代碼
	ProviderName     string           `json:"provider_name,omitempty"`
	DiagnosisCode    string           `json:"diagnosis_code,omitempty"` // ICD-10
	PharmacistID     string           `json:"pharmacist_id,omitempty"`
	PharmacistName   string           `json:"pharmacist_name,omitempty"`
	TotalPoints      float64          `json:"total_points,omitempty"`   // 總點數
	Copay            float64          `json:"copay,omitempty"`          // 部分負擔
	DataFormat       string           `json:"data_format"`              // 1=正常, 3=補正
	Items            []HISPrescriptionItem `json:"items"`
}

// HISPrescriptionItem 處方藥品項目
type HISPrescriptionItem struct {
	OrderType    string  `json:"order_type"`     // 1=藥品, 9=藥事服務費
	DrugCode     string  `json:"drug_code"`      // 健保碼
	DrugName     string  `json:"drug_name"`
	Frequency    string  `json:"frequency"`      // BID, TID...
	Route        string  `json:"route"`          // PO, EXT...
	Quantity     float64 `json:"quantity"`       // 總量
	DaysSupply   int     `json:"days_supply"`    // 天數
	UnitPrice    float64 `json:"unit_price"`     // 單價
}

// HISDrugUsage 藥品使用統計 (用於庫存分析)
type HISDrugUsage struct {
	DrugCode     string  `json:"drug_code"`
	DrugName     string  `json:"drug_name"`
	TotalQty     float64 `json:"total_qty"`
	DispenseCount int    `json:"dispense_count"`
	AvgMonthlyQty float64 `json:"avg_monthly_qty"` // 月均消耗量
}

// ============================================================================
// XML 解析函數
// ============================================================================

// ParseNHIUploadXML 解析健保每日上傳 XML (Big5 編碼)
func ParseNHIUploadXML(r io.Reader, isBig5 bool) (*HISImportResult, error) {
	result := &HISImportResult{
		SourceType:   "xml",
		SourceVendor: "nhi",
	}

	// Big5 轉 UTF-8
	var reader io.Reader = r
	if isBig5 {
		reader = transform.NewReader(r, traditionalchinese.Big5.NewDecoder())
	}

	// 解析 XML
	var xmlData NHIUploadXML
	decoder := xml.NewDecoder(reader)
	if err := decoder.Decode(&xmlData); err != nil {
		result.Errors = append(result.Errors, "XML 解析失敗: "+err.Error())
		return result, err
	}

	result.Total = len(xmlData.Records)
	patientMap := make(map[string]*HISPatient)
	drugUsageMap := make(map[string]*HISDrugUsage)

	for i, rec := range xmlData.Records {
		// 解析病患
		if rec.MB1.A12 != "" {
			patient := extractPatientFromMB1(&rec.MB1)
			if _, exists := patientMap[patient.NationalID]; !exists {
				patientMap[patient.NationalID] = patient
			}
		}

		// 解析處方
		prescription, err := extractPrescriptionFromRecord(&rec)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("第 %d 筆處方解析失敗: %s", i+1, err.Error()))
			result.Failed++
			continue
		}

		// 統計藥品使用量
		for _, item := range prescription.Items {
			if item.OrderType == "1" { // 僅統計藥品
				key := item.DrugCode
				if usage, exists := drugUsageMap[key]; exists {
					usage.TotalQty += item.Quantity
					usage.DispenseCount++
				} else {
					drugUsageMap[key] = &HISDrugUsage{
						DrugCode:      item.DrugCode,
						DrugName:      item.DrugName,
						TotalQty:      item.Quantity,
						DispenseCount: 1,
					}
				}
			}
		}

		result.Prescriptions = append(result.Prescriptions, *prescription)
		result.Imported++
	}

	// 輸出病患列表
	for _, p := range patientMap {
		result.Patients = append(result.Patients, *p)
	}

	// 輸出藥品使用統計
	for _, u := range drugUsageMap {
		result.DrugUsages = append(result.DrugUsages, *u)
	}

	result.Success = result.Failed == 0
	return result, nil
}

// extractPatientFromMB1 從 MB1 區段提取病患資料
func extractPatientFromMB1(mb1 *NHIMB1) *HISPatient {
	patient := &HISPatient{
		NationalID: strings.TrimSpace(mb1.A12),
		Name:       strings.TrimSpace(mb1.D20),
		CardNumber: strings.TrimSpace(mb1.A11),
		Phone:      strings.TrimSpace(mb1.D21),
	}

	// 民國年轉西元年 (YYYMMDD -> YYYY-MM-DD)
	if mb1.A13 != "" && len(mb1.A13) == 7 {
		patient.Birthday = convertROCDate(mb1.A13)
	}

	return patient
}

// extractPrescriptionFromRecord 從 REC 提取處方資料
func extractPrescriptionFromRecord(rec *NHIRecord) (*HISPrescription, error) {
	rx := &HISPrescription{
		PatientID:      strings.TrimSpace(rec.MB1.A12),
		ProviderCode:   strings.TrimSpace(rec.MB1.A14),
		VisitType:      strings.TrimSpace(rec.MB1.A23),
		VisitSequence:  strings.TrimSpace(rec.MB1.A18),
		DiagnosisCode:  strings.TrimSpace(rec.MB1.D19),
		PharmacistID:   strings.TrimSpace(rec.MB1.D31),
		PharmacistName: strings.TrimSpace(rec.MB1.D32),
		DataFormat:     strings.TrimSpace(rec.MB1.A01),
	}

	// 解析就診日期時間 (民國 YYYMMDDHHMMSS)
	if rec.MB1.A17 != "" && len(rec.MB1.A17) >= 7 {
		rx.DispenseDate = convertROCDate(rec.MB1.A17[:7])
		if len(rec.MB1.A17) >= 13 {
			rx.DispenseTime = rec.MB1.A17[7:9] + ":" + rec.MB1.A17[9:11] + ":" + rec.MB1.A17[11:13]
		}
	}

	// 生成處方序號
	rx.PrescriptionNo = fmt.Sprintf("%s-%s-%s", rx.ProviderCode, rx.DispenseDate, rx.VisitSequence)

	// 解析慢箋次數 (IC02 -> 2, IC03 -> 3)
	if strings.HasPrefix(rx.VisitSequence, "IC") && len(rx.VisitSequence) >= 4 {
		if n, err := strconv.Atoi(rx.VisitSequence[2:4]); err == nil {
			rx.ChronicRefillNo = n
		}
	}

	// 解析醫令明細
	for _, mb2 := range rec.MB2s {
		item := HISPrescriptionItem{
			OrderType: strings.TrimSpace(mb2.P1),
			DrugCode:  strings.TrimSpace(mb2.P2),
			DrugName:  strings.TrimSpace(mb2.P3),
			Frequency: strings.TrimSpace(mb2.P5),
			Route:     strings.TrimSpace(mb2.P6),
		}

		// 解析數值
		if mb2.P7 != "" {
			item.Quantity, _ = strconv.ParseFloat(strings.TrimSpace(mb2.P7), 64)
		}
		if mb2.P8 != "" {
			item.UnitPrice, _ = strconv.ParseFloat(strings.TrimSpace(mb2.P8), 64)
		}
		if mb2.D27 != "" {
			item.DaysSupply, _ = strconv.Atoi(strings.TrimSpace(mb2.D27))
		}

		rx.Items = append(rx.Items, item)
	}

	return rx, nil
}

// ============================================================================
// CSV 解析函數
// ============================================================================

// ParseNHIClaimCSV 解析健保費用申報 CSV (Big5 編碼)
func ParseNHIClaimCSV(r io.Reader, isBig5 bool) (*HISImportResult, error) {
	result := &HISImportResult{
		SourceType:   "csv",
		SourceVendor: "nhi",
	}

	// Big5 轉 UTF-8
	var reader io.Reader = r
	if isBig5 {
		reader = transform.NewReader(r, traditionalchinese.Big5.NewDecoder())
	}

	scanner := bufio.NewScanner(reader)
	lineNum := 0
	currentPatientID := ""
	var currentRx *HISPrescription

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		fields := strings.Split(line, ",")
		if len(fields) < 2 {
			continue
		}

		// 判斷記錄類型
		recordType := strings.TrimSpace(fields[0])

		switch {
		case recordType == "t" || recordType == "T":
			// 表頭記錄 - 跳過
			continue

		case recordType == "d" || recordType == "D":
			// 門診費用明細
			if currentRx != nil {
				result.Prescriptions = append(result.Prescriptions, *currentRx)
			}

			rx, err := parseClaimDetailLine(fields)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("第 %d 行解析失敗: %s", lineNum, err.Error()))
				result.Failed++
				currentRx = nil
				continue
			}

			currentRx = rx
			currentPatientID = rx.PatientID
			result.Total++

		case recordType == "p" || recordType == "P":
			// 醫令明細
			if currentRx == nil {
				continue
			}

			item, err := parseClaimItemLine(fields)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("第 %d 行醫令解析失敗: %s", lineNum, err.Error()))
				continue
			}

			currentRx.Items = append(currentRx.Items, *item)

			// 提取病患資訊
			if currentPatientID != "" {
				// 病患已在 d 行處理
			}
		}
	}

	// 加入最後一筆
	if currentRx != nil {
		result.Prescriptions = append(result.Prescriptions, *currentRx)
	}

	result.Imported = len(result.Prescriptions)
	result.Success = result.Failed == 0
	return result, nil
}

// parseClaimDetailLine 解析費用明細行
func parseClaimDetailLine(fields []string) (*HISPrescription, error) {
	if len(fields) < 10 {
		return nil, fmt.Errorf("欄位不足")
	}

	rx := &HISPrescription{
		PatientID: strings.TrimSpace(getField(fields, 4)),
	}

	// 案件分類
	rx.VisitType = strings.TrimSpace(getField(fields, 1))

	// 就醫日期 (民國)
	dateStr := strings.TrimSpace(getField(fields, 3))
	if len(dateStr) >= 7 {
		rx.DispenseDate = convertROCDate(dateStr)
	}

	// 流水號作為處方序號
	rx.PrescriptionNo = strings.TrimSpace(getField(fields, 2))

	// 合計點數與部分負擔
	if len(fields) > 39 {
		rx.TotalPoints, _ = strconv.ParseFloat(strings.TrimSpace(fields[39]), 64)
	}
	if len(fields) > 40 {
		rx.Copay, _ = strconv.ParseFloat(strings.TrimSpace(fields[40]), 64)
	}

	return rx, nil
}

// parseClaimItemLine 解析醫令明細行
func parseClaimItemLine(fields []string) (*HISPrescriptionItem, error) {
	if len(fields) < 8 {
		return nil, fmt.Errorf("欄位不足")
	}

	item := &HISPrescriptionItem{
		OrderType: strings.TrimSpace(getField(fields, 1)),
		DrugCode:  strings.TrimSpace(getField(fields, 2)),
		DrugName:  strings.TrimSpace(getField(fields, 3)),
	}

	// 總量
	if qtyStr := getField(fields, 7); qtyStr != "" {
		item.Quantity, _ = strconv.ParseFloat(strings.TrimSpace(qtyStr), 64)
	}

	// 單價
	if priceStr := getField(fields, 8); priceStr != "" {
		item.UnitPrice, _ = strconv.ParseFloat(strings.TrimSpace(priceStr), 64)
	}

	return item, nil
}

// ============================================================================
// 通用解析函數 (自動偵測格式)
// ============================================================================

// ParseHISFile 自動偵測並解析 HIS 匯出檔案
func ParseHISFile(r io.Reader, filename string) (*HISImportResult, error) {
	// 讀取完整內容 (需要多次解析嘗試)
	content, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("讀取檔案失敗: %w", err)
	}

	// 判斷是否為 Big5 編碼
	isBig5 := detectBig5(content)

	// 如果是 Big5，先轉換整份內容為 UTF-8
	var contentBytes []byte
	if isBig5 {
		decoded, _, err := transform.Bytes(traditionalchinese.Big5.NewDecoder(), content)
		if err != nil {
			// 轉換失敗，嘗試當作 UTF-8
			contentBytes = content
		} else {
			contentBytes = decoded
		}
	} else {
		contentBytes = content
	}

	contentStr := string(contentBytes)

	// XML 檔案
	if strings.Contains(contentStr, "<?xml") || strings.Contains(contentStr, "<RECS>") || strings.Contains(contentStr, "<REC>") {
		// XML 解析時需要原始 bytes (若為 Big5) 或已轉換的 UTF-8
		return ParseNHIUploadXML(strings.NewReader(contentStr), false)
	}

	// CSV 檔案 (健保申報格式)
	if strings.HasPrefix(strings.TrimSpace(contentStr), "t,") ||
		strings.HasPrefix(strings.TrimSpace(contentStr), "T,") ||
		strings.HasPrefix(strings.TrimSpace(contentStr), "30,") {
		return ParseNHIClaimCSV(strings.NewReader(contentStr), false)
	}

	// 通用 CSV (以逗號或 Tab 分隔)
	if strings.Contains(contentStr, ",") || strings.Contains(contentStr, "\t") {
		return parseGenericCSV(strings.NewReader(contentStr), false)
	}

	return nil, fmt.Errorf("無法識別的檔案格式")
}

// parseGenericCSV 解析通用 CSV 格式 (嘗試智慧欄位對應)
func parseGenericCSV(r io.Reader, isBig5 bool) (*HISImportResult, error) {
	result := &HISImportResult{
		SourceType:   "csv",
		SourceVendor: "generic",
	}

	var reader io.Reader = r
	if isBig5 {
		reader = transform.NewReader(r, traditionalchinese.Big5.NewDecoder())
	}

	scanner := bufio.NewScanner(reader)

	// 讀取標題行
	if !scanner.Scan() {
		return result, fmt.Errorf("檔案為空")
	}
	headerLine := scanner.Text()
	headers := parseCSVLine(headerLine)

	// 建立欄位索引對應
	colMap := buildColumnMapping(headers)

	// 用於去重的 map
	patientMap := make(map[string]*HISPatient)
	rxMap := make(map[string]*HISPrescription)

	lineNum := 1
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		fields := parseCSVLine(line)
		result.Total++

		// 嘗試提取病患
		patient := extractPatientFromCSV(fields, colMap)
		if patient != nil && patient.NationalID != "" {
			// 去重: 同一身分證只保留一筆
			if _, exists := patientMap[patient.NationalID]; !exists {
				patientMap[patient.NationalID] = patient
			}
		}

		// 嘗試提取處方箋
		rx := extractPrescriptionFromCSV(fields, colMap)
		if rx != nil && rx.PatientID != "" && rx.PrescriptionNo != "" {
			// 用處方序號去重
			key := rx.PatientID + "-" + rx.PrescriptionNo
			if _, exists := rxMap[key]; !exists {
				rxMap[key] = rx
			} else {
				// 已存在，則合併藥品項目
				if len(rx.Items) > 0 {
					rxMap[key].Items = append(rxMap[key].Items, rx.Items...)
				}
			}
		}
	}

	// 轉換 map 到 slice
	for _, p := range patientMap {
		result.Patients = append(result.Patients, *p)
	}
	for _, rx := range rxMap {
		result.Prescriptions = append(result.Prescriptions, *rx)
	}

	result.Imported = len(result.Patients) + len(result.Prescriptions)
	result.Success = result.Failed == 0
	return result, nil
}

// ============================================================================
// 輔助函數
// ============================================================================

// convertROCDate 民國年轉西元年 (YYYMMDD -> YYYY-MM-DD)
func convertROCDate(rocDate string) string {
	if len(rocDate) < 7 {
		return ""
	}

	// 提取年月日
	yearStr := rocDate[:3]
	monthStr := rocDate[3:5]
	dayStr := rocDate[5:7]

	year, err := strconv.Atoi(yearStr)
	if err != nil {
		return ""
	}

	// 民國年 + 1911 = 西元年
	adYear := year + 1911

	return fmt.Sprintf("%04d-%s-%s", adYear, monthStr, dayStr)
}

// convertROCDateTime 民國年日期時間轉西元 (YYYMMDDHHMMSS -> time.Time)
func convertROCDateTime(rocDateTime string) time.Time {
	if len(rocDateTime) < 13 {
		return time.Time{}
	}

	dateStr := convertROCDate(rocDateTime[:7])
	if dateStr == "" {
		return time.Time{}
	}

	timeStr := rocDateTime[7:9] + ":" + rocDateTime[9:11] + ":" + rocDateTime[11:13]
	t, _ := time.Parse("2006-01-02 15:04:05", dateStr+" "+timeStr)
	return t
}

// detectBig5 偵測是否為 Big5 編碼
func detectBig5(content []byte) bool {
	// 優先驗證 UTF-8：如果內容是合法 UTF-8，就不是 Big5
	// UTF-8 中文字是 3 字節序列 (0xE0-0xEF 開頭)
	utf8ValidCount := 0
	utf8InvalidCount := 0

	for i := 0; i < len(content); {
		b := content[i]

		// ASCII 範圍
		if b < 0x80 {
			i++
			continue
		}

		// UTF-8 3 字節序列 (中文字)
		if b >= 0xE0 && b <= 0xEF && i+2 < len(content) {
			b2, b3 := content[i+1], content[i+2]
			// 驗證後續字節是 10xxxxxx 格式
			if (b2&0xC0) == 0x80 && (b3&0xC0) == 0x80 {
				utf8ValidCount++
				i += 3
				continue
			}
		}

		// UTF-8 2 字節序列
		if b >= 0xC0 && b <= 0xDF && i+1 < len(content) {
			b2 := content[i+1]
			if (b2 & 0xC0) == 0x80 {
				utf8ValidCount++
				i += 2
				continue
			}
		}

		// 不是合法 UTF-8 序列
		utf8InvalidCount++
		i++
	}

	// 如果有大量合法 UTF-8 序列且幾乎沒有非法序列，則為 UTF-8
	if utf8ValidCount > 5 && utf8InvalidCount < utf8ValidCount/10 {
		return false // 是 UTF-8，不是 Big5
	}

	// 否則嘗試檢測 Big5
	big5Count := 0
	for i := 0; i < len(content)-1; i++ {
		b1, b2 := content[i], content[i+1]
		// Big5 雙字節範圍
		if b1 >= 0x81 && b1 <= 0xFE {
			if (b2 >= 0x40 && b2 <= 0x7E) || (b2 >= 0xA1 && b2 <= 0xFE) {
				big5Count++
				i++
			}
		}
	}

	return big5Count > 5
}

// buildColumnMapping 建立欄位名稱對應索引
func buildColumnMapping(headers []string) map[string]int {
	colMap := make(map[string]int)

	// 常見欄位名稱對應
	patterns := map[string][]string{
		"national_id":     {"身分證", "身份證", "ID", "national_id", "pid", "病患ID", "idno"},
		"name":            {"姓名", "name", "patient_name", "病患姓名"},
		"birthday":        {"生日", "出生日期", "birthday", "dob", "birth"},
		"phone":           {"電話", "phone", "tel", "手機", "mobile"},
		"drug_code":       {"藥品代碼", "健保碼", "drug_code", "code", "nhi_code"},
		"drug_name":       {"藥品名稱", "drug_name", "藥名"},
		"quantity":        {"數量", "總量", "quantity", "qty"},
		"days":            {"天數", "日份", "給藥天數", "給藥日數", "days", "day"},
		"prescription_no": {"處方箋號", "處方號", "處方箋", "prescription_no", "rx_no", "rxno"},
		"visit_date":      {"就診日", "就診日期", "調劑日期", "visit_date", "dispense_date", "date"},
		"visit_type":      {"就醫類別", "visit_type", "type"},
		"hospital":        {"醫院", "hospital", "provider", "來源醫院"},
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

// extractPatientFromCSV 從 CSV 行提取病患資料
func extractPatientFromCSV(fields []string, colMap map[string]int) *HISPatient {
	patient := &HISPatient{}

	if idx, ok := colMap["national_id"]; ok && idx < len(fields) {
		patient.NationalID = strings.TrimSpace(fields[idx])
	}
	if idx, ok := colMap["name"]; ok && idx < len(fields) {
		patient.Name = strings.TrimSpace(fields[idx])
	}
	if idx, ok := colMap["birthday"]; ok && idx < len(fields) {
		birthday := strings.TrimSpace(fields[idx])
		// 嘗試轉換民國年
		if len(birthday) == 7 && birthday[0] >= '0' && birthday[0] <= '1' {
			patient.Birthday = convertROCDate(birthday)
		} else {
			patient.Birthday = birthday
		}
	}
	if idx, ok := colMap["phone"]; ok && idx < len(fields) {
		patient.Phone = strings.TrimSpace(fields[idx])
	}

	return patient
}

// extractPrescriptionFromCSV 從 CSV 行提取處方箋資料
func extractPrescriptionFromCSV(fields []string, colMap map[string]int) *HISPrescription {
	rx := &HISPrescription{}

	// 病患身分證
	if idx, ok := colMap["national_id"]; ok && idx < len(fields) {
		rx.PatientID = strings.TrimSpace(fields[idx])
	}

	// 處方箋號
	if idx, ok := colMap["prescription_no"]; ok && idx < len(fields) {
		rx.PrescriptionNo = strings.TrimSpace(fields[idx])
	}

	// 就診日期
	if idx, ok := colMap["visit_date"]; ok && idx < len(fields) {
		dateStr := strings.TrimSpace(fields[idx])
		// 嘗試轉換民國年
		if len(dateStr) == 7 && dateStr[0] >= '0' && dateStr[0] <= '1' {
			rx.DispenseDate = convertROCDate(dateStr)
		} else {
			rx.DispenseDate = dateStr
		}
	}

	// 就醫類別
	if idx, ok := colMap["visit_type"]; ok && idx < len(fields) {
		rx.VisitType = strings.TrimSpace(fields[idx])
	}

	// 醫院
	if idx, ok := colMap["hospital"]; ok && idx < len(fields) {
		rx.ProviderName = strings.TrimSpace(fields[idx])
	}

	// 藥品項目
	item := HISPrescriptionItem{}
	if idx, ok := colMap["drug_code"]; ok && idx < len(fields) {
		item.DrugCode = strings.TrimSpace(fields[idx])
	}
	if idx, ok := colMap["drug_name"]; ok && idx < len(fields) {
		item.DrugName = strings.TrimSpace(fields[idx])
	}
	if idx, ok := colMap["quantity"]; ok && idx < len(fields) {
		item.Quantity, _ = strconv.ParseFloat(strings.TrimSpace(fields[idx]), 64)
	}
	if idx, ok := colMap["days"]; ok && idx < len(fields) {
		item.DaysSupply, _ = strconv.Atoi(strings.TrimSpace(fields[idx]))
	}

	if item.DrugCode != "" {
		rx.Items = append(rx.Items, item)
	}

	// 判斷慢箋: 就醫類別 08 或天數 >= 28
	if rx.VisitType == "08" || (len(rx.Items) > 0 && rx.Items[0].DaysSupply >= 28) {
		rx.ChronicRefillNo = 1 // 預設第一次
	}

	return rx
}

// parseCSVLine 解析 CSV 行 (處理引號)
func parseCSVLine(line string) []string {
	var fields []string
	var field strings.Builder
	inQuotes := false

	for _, r := range line {
		switch {
		case r == '"':
			inQuotes = !inQuotes
		case r == ',' && !inQuotes:
			fields = append(fields, field.String())
			field.Reset()
		default:
			field.WriteRune(r)
		}
	}
	fields = append(fields, field.String())

	return fields
}

// getField 安全取得欄位值
func getField(fields []string, index int) string {
	if index >= 0 && index < len(fields) {
		return fields[index]
	}
	return ""
}

// min 取最小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ============================================================================
// 通用匯入功能 (病患/庫存/健保藥品主檔)
// ============================================================================

// ImportResult 匯入結果統計
type ImportResult struct {
	Total   int      `json:"total"`
	Success int      `json:"success"`
	Errors  []string `json:"errors"`
}

// PatientImport 病患匯入資料
type PatientImport struct {
	NationalID string
	Name       string
	Birthday   string
	Phone      string
	Address    string
	Notes      string
}

// InventoryImport 庫存匯入資料
type InventoryImport struct {
	DrugCode     string
	DrugName     string
	CurrentStock float64
	MinStock     float64
	Supplier     string
	UnitPrice    float64
	Notes        string
}

// NHIDrugImport 健保藥品匯入資料
type NHIDrugImport struct {
	DrugCode string
	DrugName string
	Supplier string
}

// ParsePatientCSV 解析病患 CSV 檔案
// CSV 欄位順序: 身分證號,姓名,生日,電話,地址,備註
func ParsePatientCSV(r io.Reader) (*ImportResult, []PatientImport) {
	result := &ImportResult{Errors: []string{}}
	var patients []PatientImport

	// 嘗試偵測編碼
	content, _ := io.ReadAll(r)
	var reader io.Reader
	if detectBig5(content) {
		reader = transform.NewReader(strings.NewReader(string(content)), traditionalchinese.Big5.NewDecoder())
	} else {
		reader = strings.NewReader(string(content))
	}

	scanner := bufio.NewScanner(reader)
	lineNo := 0

	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		result.Total++

		// 跳過表頭
		if lineNo == 1 && (strings.Contains(line, "身分證") || strings.Contains(line, "姓名") || strings.Contains(line, "national_id")) {
			result.Total--
			continue
		}

		fields := parseCSVLine(line)
		if len(fields) < 2 {
			result.Errors = append(result.Errors, fmt.Sprintf("第 %d 行格式錯誤", lineNo))
			continue
		}

		patient := PatientImport{
			NationalID: strings.TrimSpace(getField(fields, 0)),
			Name:       strings.TrimSpace(getField(fields, 1)),
			Birthday:   strings.TrimSpace(getField(fields, 2)),
			Phone:      strings.TrimSpace(getField(fields, 3)),
			Address:    strings.TrimSpace(getField(fields, 4)),
			Notes:      strings.TrimSpace(getField(fields, 5)),
		}

		if patient.NationalID == "" || patient.Name == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("第 %d 行缺少必要欄位", lineNo))
			continue
		}

		patients = append(patients, patient)
		result.Success++
	}

	return result, patients
}

// ParseInventoryCSV 解析庫存 CSV 檔案
// CSV 欄位順序: 藥品代碼,藥品名稱,現有庫存,安全庫存,供應商,單價,備註
func ParseInventoryCSV(r io.Reader) (*ImportResult, []InventoryImport) {
	result := &ImportResult{Errors: []string{}}
	var items []InventoryImport

	// 嘗試偵測編碼
	content, _ := io.ReadAll(r)
	var reader io.Reader
	if detectBig5(content) {
		reader = transform.NewReader(strings.NewReader(string(content)), traditionalchinese.Big5.NewDecoder())
	} else {
		reader = strings.NewReader(string(content))
	}

	scanner := bufio.NewScanner(reader)
	lineNo := 0

	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		result.Total++

		// 跳過表頭
		if lineNo == 1 && (strings.Contains(line, "藥品代碼") || strings.Contains(line, "藥品名稱") || strings.Contains(line, "drug_code")) {
			result.Total--
			continue
		}

		fields := parseCSVLine(line)
		if len(fields) < 2 {
			result.Errors = append(result.Errors, fmt.Sprintf("第 %d 行格式錯誤", lineNo))
			continue
		}

		item := InventoryImport{
			DrugCode: strings.TrimSpace(getField(fields, 0)),
			DrugName: strings.TrimSpace(getField(fields, 1)),
		}

		// 解析數值欄位
		if qty := getField(fields, 2); qty != "" {
			item.CurrentStock, _ = strconv.ParseFloat(qty, 64)
		}
		if safety := getField(fields, 3); safety != "" {
			item.MinStock, _ = strconv.ParseFloat(safety, 64)
		}
		item.Supplier = strings.TrimSpace(getField(fields, 4))
		if price := getField(fields, 5); price != "" {
			item.UnitPrice, _ = strconv.ParseFloat(price, 64)
		}
		item.Notes = strings.TrimSpace(getField(fields, 6))

		if item.DrugCode == "" || item.DrugName == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("第 %d 行缺少必要欄位", lineNo))
			continue
		}

		items = append(items, item)
		result.Success++
	}

	return result, items
}

// ParseNHIDrugFile 解析健保藥品主檔
// CSV 欄位順序: 健保碼,藥品名稱,廠商...
func ParseNHIDrugFile(r io.Reader) (*ImportResult, []NHIDrugImport) {
	result := &ImportResult{Errors: []string{}}
	var items []NHIDrugImport

	// 嘗試偵測編碼
	content, _ := io.ReadAll(r)
	var reader io.Reader
	if detectBig5(content) {
		reader = transform.NewReader(strings.NewReader(string(content)), traditionalchinese.Big5.NewDecoder())
	} else {
		reader = strings.NewReader(string(content))
	}

	scanner := bufio.NewScanner(reader)
	lineNo := 0

	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		result.Total++

		// 跳過表頭
		if lineNo == 1 && (strings.Contains(line, "健保碼") || strings.Contains(line, "藥品代碼") || strings.Contains(line, "代碼")) {
			result.Total--
			continue
		}

		fields := parseCSVLine(line)
		if len(fields) < 2 {
			result.Errors = append(result.Errors, fmt.Sprintf("第 %d 行格式錯誤", lineNo))
			continue
		}

		item := NHIDrugImport{
			DrugCode: strings.TrimSpace(getField(fields, 0)),
			DrugName: strings.TrimSpace(getField(fields, 1)),
			Supplier: strings.TrimSpace(getField(fields, 2)),
		}

		if item.DrugCode == "" || item.DrugName == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("第 %d 行缺少必要欄位", lineNo))
			continue
		}

		items = append(items, item)
		result.Success++
	}

	return result, items
}
