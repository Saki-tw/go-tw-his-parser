//go:build js && wasm

package main

import (
	"encoding/json"
	"strings"
	"syscall/js"

	parser "github.com/Saki-tw/go-tw-his-parser"
)

// parseHISData 解析 HIS 資料並返回 JSON
func parseHISData(this js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return map[string]interface{}{
			"success": false,
			"error":   "請提供要解析的資料",
		}
	}

	content := args[0].String()
	filename := "input.txt"
	if len(args) >= 2 {
		filename = args[1].String()
	}

	// 解析資料
	result, err := parser.ParseHISFileAuto(strings.NewReader(content), filename)
	if err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		}
	}

	// 轉換為 JSON
	jsonBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   "JSON 編碼失敗: " + err.Error(),
		}
	}

	return map[string]interface{}{
		"success": true,
		"data":    string(jsonBytes),
		"summary": map[string]interface{}{
			"patients":      len(result.Patients),
			"prescriptions": len(result.Prescriptions),
			"sourceType":    result.SourceType,
			"sourceVendor":  result.SourceVendor,
		},
	}
}

// getSupportedVendors 取得支援的廠商列表
func getSupportedVendors(this js.Value, args []js.Value) interface{} {
	vendors := parser.GetSupportedVendors()
	jsonBytes, _ := json.Marshal(vendors)
	return string(jsonBytes)
}

func main() {
	c := make(chan struct{}, 0)

	// 註冊全域函數
	js.Global().Set("parseHISData", js.FuncOf(parseHISData))
	js.Global().Set("getSupportedVendors", js.FuncOf(getSupportedVendors))

	// 設定 ready 標誌
	js.Global().Set("wasmReady", true)

	println("go-tw-his-parser WASM 模組已載入")

	// 保持程式運行
	<-c
}
