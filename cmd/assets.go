package main

import _ "embed"

// 三套主题的应用图标（品牌圆角方块 + 白色 W + 放大镜，与控制台 UI 同源设计），
// 由 tmp/genicon 一次性生成后入库；桌面快捷方式按 dashboard.brand.theme
// 选择对应图标落盘引用。
var (
	//go:embed assets/app-green.ico
	appGreenICO []byte

	//go:embed assets/app-blue.ico
	appBlueICO []byte

	//go:embed assets/app-mono.ico
	appMonoICO []byte
)
