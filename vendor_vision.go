// Package parser 展望 HIS 資料格式解析器
// 展望為台灣市佔率最高的藥局 HIS 系統之一
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
// 展望 HIS 專屬格式定義
// ============================================================================

// VisionExportType 展望匯出類型
type VisionExportType string

const (
	VisionXML    VisionExportType = "xml"    // 健保每日上傳 XML
	VisionCSV    VisionExportType = "csv"    // 月申報 CSV
	VisionDRUG   VisionExportType = "drug"   // 藥品主檔
	VisionPTNT   VisionExportType = "ptnt"   // 病患主檔
)

// VisionXMLRoot 展望 XML 根元素
type VisionXMLRoot struct {
	XMLName xml.Name     `xml:"RECS"`
	Records []VisionRec  `xml:"REC"`
}

// VisionRec 展望單筆記錄 (展望格式與健保標準較接近，但有額外欄位)
type VisionRec struct {
	// MSH 表頭
	MSH struct {
		H1 string `xml:"h1"` // 醫事機構代號
		H2 string `xml:"h2"` // 費用年月
		H3 string `xml:"h3"` // 申報類別
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
		D22 string `xml:"d22"` // 病患地址 (展望特有)
		D31 string `xml:"d31"` // 藥師身分證
		D32 string `xml:"d32"` // 藥師姓名
	} `xml:"MB1"`

	// MB2 醫令明細
	MB2s []struct {
		P1  string `xml:"p1"`  // 醫令類別
		P2  string `xml:"p2"`  // 藥品代碼
		P3  string `xml:"p3"`  // 藥品名稱
		P4  string `xml:"p4"`  // 規格 (展望特有)
		P5  string `xml:"p5"`  // 使用頻率
		P6  string `xml:"p6"`  // 給藥途徑
		P7  string `xml:"p7"`  // 總量
		P8  string `xml:"p8"`  // 單價
		D27 string `xml:"d27"` // 給藥天數
		D28 string `xml:"d28"` // 單次劑量 (展望特有)
		D36 string `xml:"d36"` // 慢箋次數
	} `xml:"MB2"`
}

// VisionCSVRecord 展望 CSV 記錄格式
// 展望月申報 CSV 使用 T/D/P 記錄類型
type VisionCSVRecord struct {
	RecordType    string  // T=表頭, D=門診明細, P=醫令
	CaseType      string  // 案件分類
	SeqNo         string  // 流水號
	VisitDate     string  // 就診日期
	PatientID     string  // 身分證
	PatientName   string  // 姓名
	DrugCode      string  // 藥品代碼
	DrugName      string  // 藥品名稱
	Quantity      float64 // 數量
	UnitPrice     float64 // 單價
	TotalPoints   float64 // 點數
	Copay         float64 // 部分負擔
}

// ============================================================================
// 展望解析器
// ============================================================================

// ParseVisionFile 解析展望 HIS 匯出檔案
func ParseVisionFile(r io.Reader, filename string) (*HISImportResult, error) {
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
		return parseVisionXML(contentStr)
	}

	// CSV 格式
	return parseVisionCSV(contentStr)
}

// parseVisionXML 解析展望 XML 格式
func parseVisionXML(content string) (*HISImportResult, error) {
	result := &HISImportResult{
		SourceType:   "xml",
		SourceVendor: "vision",
	}

	var xmlData VisionXMLRoot
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
				Phone:      strings.TrimSpace(rec.MB1.D21),
			}
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

		// 生成處方序號 (展望前綴 VS)
		rx.PrescriptionNo = fmt.Sprintf("VS-%s-%s-%s", rx.ProviderCode, rx.DispenseDate, rx.VisitSequence)

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

// parseVisionCSV 解析展望 CSV 格式 (健保申報格式 T/D/P)
func parseVisionCSV(content string) (*HISImportResult, error) {
	result := &HISImportResult{
		SourceType:   "csv",
		SourceVendor: "vision",
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

		fields := parseCSVLine(line)
		if len(fields) < 2 {
			continue
		}

		recordType := strings.ToUpper(strings.TrimSpace(fields[0]))

		switch recordType {
		case "T":
			// 表頭記錄 - 跳過
			continue

		case "D":
			// 門診費用明細
			result.Total++

			if len(fields) < 10 {
				result.Errors = append(result.Errors, fmt.Sprintf("第 %d 行欄位不足", lineNum))
				result.Failed++
				continue
			}

			// 展望 D 行格式: D,案件,流水號,就診日,身分證,姓名,...
			caseType := strings.TrimSpace(getField(fields, 1))
			seqNo := strings.TrimSpace(getField(fields, 2))
			visitDate := strings.TrimSpace(getField(fields, 3))
			nationalID := strings.TrimSpace(getField(fields, 4))
			name := strings.TrimSpace(getField(fields, 5))

			// 建立病患
			if nationalID != "" {
				if _, exists := patientMap[nationalID]; !exists {
					patientMap[nationalID] = &HISPatient{
						NationalID: nationalID,
						Name:       name,
					}
				}
			}

			// 建立處方
			rxKey := nationalID + "-" + seqNo
			currentRxKey = rxKey

			dispenseDate := visitDate
			if len(visitDate) == 7 {
				dispenseDate = convertROCDate(visitDate)
			}

			rxMap[rxKey] = &HISPrescription{
				PatientID:      nationalID,
				PrescriptionNo: fmt.Sprintf("VS-%s", seqNo),
				DispenseDate:   dispenseDate,
				VisitType:      caseType,
			}

			// 慢箋判斷
			if caseType == "08" {
				rxMap[rxKey].ChronicRefillNo = 1
			}

			// 總點數與部分負擔 (若有)
			if len(fields) > 39 {
				rxMap[rxKey].TotalPoints, _ = strconv.ParseFloat(strings.TrimSpace(fields[39]), 64)
			}
			if len(fields) > 40 {
				rxMap[rxKey].Copay, _ = strconv.ParseFloat(strings.TrimSpace(fields[40]), 64)
			}

			result.Imported++

		case "P":
			// 醫令明細
			if currentRxKey == "" {
				continue
			}

			if len(fields) < 8 {
				continue
			}

			// 展望 P 行格式: P,醫令類別,藥品代碼,藥品名稱,...,總量,單價
			orderType := strings.TrimSpace(getField(fields, 1))
			drugCode := strings.TrimSpace(getField(fields, 2))
			drugName := strings.TrimSpace(getField(fields, 3))
			qtyStr := getField(fields, 7)
			priceStr := getField(fields, 8)

			item := HISPrescriptionItem{
				OrderType: orderType,
				DrugCode:  drugCode,
				DrugName:  drugName,
			}

			if qtyStr != "" {
				item.Quantity, _ = strconv.ParseFloat(strings.TrimSpace(qtyStr), 64)
			}
			if priceStr != "" {
				item.UnitPrice, _ = strconv.ParseFloat(strings.TrimSpace(priceStr), 64)
			}

			if rx, exists := rxMap[currentRxKey]; exists {
				rx.Items = append(rx.Items, item)
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
