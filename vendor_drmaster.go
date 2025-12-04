// Package parser 看診大師 HIS 資料格式解析器
// 看診大師為台灣常見的診所/藥局 HIS 系統
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
// 看診大師 HIS 專屬格式定義
// ============================================================================

// DrMasterExportType 看診大師匯出類型
type DrMasterExportType string

const (
	DrMasterXML  DrMasterExportType = "xml"  // 健保每日上傳 XML
	DrMasterCSV  DrMasterExportType = "csv"  // 月申報 CSV
	DrMasterTXT  DrMasterExportType = "txt"  // 文字報表
	DrMasterDBF  DrMasterExportType = "dbf"  // dBASE 格式 (較舊版本)
)

// DrMasterXMLRoot 看診大師 XML 根元素
type DrMasterXMLRoot struct {
	XMLName xml.Name      `xml:"RECS"`
	Records []DrMasterRec `xml:"REC"`
}

// DrMasterRec 看診大師單筆記錄
type DrMasterRec struct {
	// MSH 表頭
	MSH struct {
		H1 string `xml:"h1"` // 醫事機構代號
		H2 string `xml:"h2"` // 費用年月
		H3 string `xml:"h3"` // 申報類別
		H4 string `xml:"h4"` // 系統版本 (看診大師特有)
	} `xml:"MSH"`

	// MB1 就醫基本資料
	MB1 struct {
		A01 string `xml:"A01"` // 資料格式
		A11 string `xml:"A11"` // 健保卡號
		A12 string `xml:"A12"` // 身分證
		A13 string `xml:"A13"` // 出生日期
		A14 string `xml:"A14"` // 原處方醫院
		A17 string `xml:"A17"` // 就診日期時間
		A18 string `xml:"A18"` // 就醫序號
		A23 string `xml:"A23"` // 就醫類別
		D19 string `xml:"d19"` // 診斷碼
		D20 string `xml:"d20"` // 病患姓名
		D21 string `xml:"d21"` // 病患電話
		D23 string `xml:"d23"` // 病患手機 (看診大師特有)
		D24 string `xml:"d24"` // 緊急聯絡人 (看診大師特有)
		D31 string `xml:"d31"` // 藥師身分證
		D32 string `xml:"d32"` // 藥師姓名
	} `xml:"MB1"`

	// MB2 醫令明細
	MB2s []struct {
		P1  string `xml:"p1"`  // 醫令類別
		P2  string `xml:"p2"`  // 藥品代碼
		P3  string `xml:"p3"`  // 藥品名稱
		P4  string `xml:"p4"`  // 成分/規格
		P5  string `xml:"p5"`  // 使用頻率
		P6  string `xml:"p6"`  // 給藥途徑
		P7  string `xml:"p7"`  // 總量
		P8  string `xml:"p8"`  // 單價
		P9  string `xml:"p9"`  // 成本價 (看診大師特有)
		D27 string `xml:"d27"` // 給藥天數
		D28 string `xml:"d28"` // 單次劑量
		D29 string `xml:"d29"` // 單位 (看診大師特有)
		D36 string `xml:"d36"` // 慢箋次數
		D37 string `xml:"d37"` // 連處總次數 (看診大師特有)
	} `xml:"MB2"`
}

// DrMasterTXTRecord 看診大師文字報表格式 (固定分隔符)
// 看診大師使用 | 作為欄位分隔符
type DrMasterTXTRecord struct {
	RecordType   string // H=表頭, D=病患資料, M=藥品明細
	HospitalCode string
	NationalID   string
	PatientName  string
	Birthday     string
	Phone        string
	VisitDate    string
	DrugCode     string
	DrugName     string
	Quantity     float64
	Days         int
	Frequency    string
	VisitType    string
}

// ============================================================================
// 看診大師解析器
// ============================================================================

// ParseDrMasterFile 解析看診大師 HIS 匯出檔案
func ParseDrMasterFile(r io.Reader, filename string) (*HISImportResult, error) {
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

	lowerFilename := strings.ToLower(filename)

	// XML 格式
	if strings.HasSuffix(lowerFilename, ".xml") ||
	   strings.Contains(contentStr, "<?xml") ||
	   strings.Contains(contentStr, "<RECS>") {
		return parseDrMasterXML(contentStr)
	}

	// TXT 格式 (使用 | 分隔)
	if strings.Contains(contentStr, "|") {
		return parseDrMasterTXT(contentStr)
	}

	// CSV 格式
	return parseDrMasterCSV(contentStr)
}

// parseDrMasterXML 解析看診大師 XML 格式
func parseDrMasterXML(content string) (*HISImportResult, error) {
	result := &HISImportResult{
		SourceType:   "xml",
		SourceVendor: "drmaster",
	}

	var xmlData DrMasterXMLRoot
	if err := xml.Unmarshal([]byte(content), &xmlData); err != nil {
		result.Errors = append(result.Errors, "XML 解析失敗: "+err.Error())
		return result, err
	}

	result.Total = len(xmlData.Records)
	patientMap := make(map[string]*HISPatient)

	for i, rec := range xmlData.Records {
		// 提取病患
		if rec.MB1.A12 != "" {
			patient := &HISPatient{
				NationalID: strings.TrimSpace(rec.MB1.A12),
				Name:       strings.TrimSpace(rec.MB1.D20),
				CardNumber: strings.TrimSpace(rec.MB1.A11),
			}

			// 電話：優先使用手機
			phone := strings.TrimSpace(rec.MB1.D23)
			if phone == "" {
				phone = strings.TrimSpace(rec.MB1.D21)
			}
			patient.Phone = phone

			if rec.MB1.A13 != "" && len(rec.MB1.A13) >= 7 {
				patient.Birthday = convertROCDate(rec.MB1.A13[:7])
			}
			if _, exists := patientMap[patient.NationalID]; !exists {
				patientMap[patient.NationalID] = patient
			}
		}

		// 提取處方
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

		// 解析就診日期時間
		if rec.MB1.A17 != "" && len(rec.MB1.A17) >= 7 {
			rx.DispenseDate = convertROCDate(rec.MB1.A17[:7])
			if len(rec.MB1.A17) >= 13 {
				rx.DispenseTime = rec.MB1.A17[7:9] + ":" + rec.MB1.A17[9:11] + ":" + rec.MB1.A17[11:13]
			}
		}

		// 生成處方序號 (看診大師前綴 DM)
		rx.PrescriptionNo = fmt.Sprintf("DM-%s-%s-%s", rx.ProviderCode, rx.DispenseDate, rx.VisitSequence)

		// 解析慢箋次數
		if strings.HasPrefix(rx.VisitSequence, "IC") && len(rx.VisitSequence) >= 4 {
			if n, err := strconv.Atoi(rx.VisitSequence[2:4]); err == nil {
				rx.ChronicRefillNo = n
			}
		}

		// 解析藥品項目
		for _, mb2 := range rec.MB2s {
			item := HISPrescriptionItem{
				OrderType: strings.TrimSpace(mb2.P1),
				DrugCode:  strings.TrimSpace(mb2.P2),
				DrugName:  strings.TrimSpace(mb2.P3),
				Frequency: strings.TrimSpace(mb2.P5),
				Route:     strings.TrimSpace(mb2.P6),
			}
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

// parseDrMasterTXT 解析看診大師 TXT 格式 (使用 | 分隔)
func parseDrMasterTXT(content string) (*HISImportResult, error) {
	result := &HISImportResult{
		SourceType:   "txt",
		SourceVendor: "drmaster",
	}

	scanner := bufio.NewScanner(strings.NewReader(content))
	patientMap := make(map[string]*HISPatient)
	rxMap := make(map[string]*HISPrescription)
	lineNum := 0
	var currentRxKey string

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// 看診大師使用 | 作為分隔符
		fields := strings.Split(line, "|")
		if len(fields) < 2 {
			continue
		}

		recordType := strings.ToUpper(strings.TrimSpace(fields[0]))

		switch recordType {
		case "H":
			// 表頭記錄 - 跳過
			continue

		case "D":
			// 病患資料行
			result.Total++

			if len(fields) < 7 {
				result.Errors = append(result.Errors, fmt.Sprintf("第 %d 行欄位不足", lineNum))
				result.Failed++
				continue
			}

			// 看診大師 D 行格式: D|身分證|姓名|生日|電話|就診日|就醫類別
			nationalID := strings.TrimSpace(fields[1])
			name := strings.TrimSpace(fields[2])
			birthday := strings.TrimSpace(fields[3])
			phone := strings.TrimSpace(fields[4])
			visitDate := strings.TrimSpace(fields[5])
			visitType := ""
			if len(fields) > 6 {
				visitType = strings.TrimSpace(fields[6])
			}

			// 建立病患
			if nationalID != "" {
				if _, exists := patientMap[nationalID]; !exists {
					patient := &HISPatient{
						NationalID: nationalID,
						Name:       name,
						Phone:      phone,
					}
					if len(birthday) == 7 {
						patient.Birthday = convertROCDate(birthday)
					} else {
						patient.Birthday = birthday
					}
					patientMap[nationalID] = patient
				}
			}

			// 建立處方
			rxKey := nationalID + "-" + visitDate
			currentRxKey = rxKey

			dispenseDate := visitDate
			if len(visitDate) == 7 {
				dispenseDate = convertROCDate(visitDate)
			}

			rxMap[rxKey] = &HISPrescription{
				PatientID:      nationalID,
				PrescriptionNo: fmt.Sprintf("DM-%s-%s", nationalID, visitDate),
				DispenseDate:   dispenseDate,
				VisitType:      visitType,
			}

			// 慢箋判斷
			if visitType == "08" {
				rxMap[rxKey].ChronicRefillNo = 1
			}

			result.Imported++

		case "M":
			// 藥品明細行
			if currentRxKey == "" {
				continue
			}

			if len(fields) < 5 {
				continue
			}

			// 看診大師 M 行格式: M|藥品代碼|藥品名稱|數量|天數|頻率
			drugCode := strings.TrimSpace(fields[1])
			drugName := strings.TrimSpace(fields[2])
			qtyStr := fields[3]
			daysStr := ""
			frequency := ""
			if len(fields) > 4 {
				daysStr = fields[4]
			}
			if len(fields) > 5 {
				frequency = strings.TrimSpace(fields[5])
			}

			qty, _ := strconv.ParseFloat(strings.TrimSpace(qtyStr), 64)
			days, _ := strconv.Atoi(strings.TrimSpace(daysStr))

			item := HISPrescriptionItem{
				OrderType:  "1",
				DrugCode:   drugCode,
				DrugName:   drugName,
				Quantity:   qty,
				DaysSupply: days,
				Frequency:  frequency,
			}

			if rx, exists := rxMap[currentRxKey]; exists {
				rx.Items = append(rx.Items, item)

				// 若天數 >= 28，視為慢箋
				if days >= 28 && rx.ChronicRefillNo == 0 {
					rx.ChronicRefillNo = 1
				}
			}
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

// parseDrMasterCSV 解析看診大師 CSV 格式
func parseDrMasterCSV(content string) (*HISImportResult, error) {
	result := &HISImportResult{
		SourceType:   "csv",
		SourceVendor: "drmaster",
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
			if isDrMasterHeaderLine(fields) {
				headers = fields
				colMap = buildDrMasterColumnMapping(headers)
				continue
			}
			colMap = getDrMasterDefaultColumns()
		}

		result.Total++

		// 提取資料
		nationalID := getFieldByKey(fields, colMap, "national_id")
		name := getFieldByKey(fields, colMap, "name")
		birthday := getFieldByKey(fields, colMap, "birthday")
		phone := getFieldByKey(fields, colMap, "phone")
		visitDate := getFieldByKey(fields, colMap, "visit_date")
		drugCode := getFieldByKey(fields, colMap, "drug_code")
		drugName := getFieldByKey(fields, colMap, "drug_name")
		qtyStr := getFieldByKey(fields, colMap, "quantity")
		daysStr := getFieldByKey(fields, colMap, "days")
		visitType := getFieldByKey(fields, colMap, "visit_type")
		frequency := getFieldByKey(fields, colMap, "frequency")

		// 建立病患
		if nationalID != "" {
			if _, exists := patientMap[nationalID]; !exists {
				patient := &HISPatient{
					NationalID: nationalID,
					Name:       name,
					Phone:      phone,
				}
				if len(birthday) == 7 {
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
					PrescriptionNo: fmt.Sprintf("DM-%s-%s", nationalID, visitDate),
					DispenseDate:   dispenseDate,
					VisitType:      visitType,
				}

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
					Frequency:  frequency,
				})

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

// isDrMasterHeaderLine 判斷是否為看診大師 CSV 標題行
func isDrMasterHeaderLine(fields []string) bool {
	if len(fields) < 3 {
		return false
	}

	headerKeywords := []string{"身分證", "姓名", "藥品", "日期", "代碼", "處方"}
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

// buildDrMasterColumnMapping 建立看診大師欄位對應
func buildDrMasterColumnMapping(headers []string) map[string]int {
	colMap := make(map[string]int)

	patterns := map[string][]string{
		"national_id": {"身分證", "身份證", "ID", "pid"},
		"name":        {"姓名", "name", "patient"},
		"birthday":    {"生日", "出生", "birthday"},
		"phone":       {"電話", "手機", "phone", "mobile"},
		"visit_date":  {"就診日", "調劑日", "日期", "date"},
		"drug_code":   {"藥品代碼", "藥碼", "健保碼", "code"},
		"drug_name":   {"藥品名稱", "藥名", "drug"},
		"quantity":    {"數量", "總量", "qty"},
		"days":        {"天數", "日份", "days"},
		"visit_type":  {"就醫類別", "案件", "type"},
		"frequency":   {"頻率", "使用頻率", "freq"},
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

// getDrMasterDefaultColumns 取得看診大師預設欄位順序
func getDrMasterDefaultColumns() map[string]int {
	// 看診大師常見匯出順序
	return map[string]int{
		"national_id": 0,
		"name":        1,
		"birthday":    2,
		"phone":       3,
		"visit_date":  4,
		"drug_code":   5,
		"drug_name":   6,
		"quantity":    7,
		"days":        8,
		"visit_type":  9,
		"frequency":   10,
	}
}
