// 一次性图标生成器：产出三套主题的 cmd/assets/app-{green,blue,mono}.ico
// 与 pkg/dashboard/web/logo-{green,blue,mono}.png（WebUI 默认品牌 logo）。
// 设计语言：品牌色圆角方块 + 白色 W（网络搜索 WebSearch 首字母）
// + 右下角白色放大镜（搜索语义）。
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

type theme struct {
	main  color.RGBA
	light color.RGBA
}

var themes = map[string]theme{
	"green": {main: color.RGBA{R: 0x14, G: 0x63, B: 0x3f, A: 0xff}, light: color.RGBA{R: 0x1e, G: 0x7d, B: 0x51, A: 0xff}},
	"blue":  {main: color.RGBA{R: 0x1e, G: 0x5d, B: 0xb8, A: 0xff}, light: color.RGBA{R: 0x2f, G: 0x74, B: 0xd0, A: 0xff}},
	"mono":  {main: color.RGBA{R: 0x24, G: 0x28, B: 0x2b, A: 0xff}, light: color.RGBA{R: 0x3d, G: 0x44, B: 0x49, A: 0xff}},
}

var white = color.RGBA{R: 255, G: 255, B: 255, A: 255}

// polyline 定义 W 的四个线段（单位坐标，y 向下；整体左上偏移给放大镜留位）
var wpts = [][2]float64{
	{0.10, 0.22}, {0.24, 0.62}, {0.38, 0.28}, {0.52, 0.62}, {0.64, 0.22},
}

// 放大镜：镜圈圆心/半径、柄线段（缩小并收进右下角，与 W 完全分离），
// 镜片内为白色圆盘 + 主题色地球仪经纬线（网络语义）。
var lensC = [2]float64{0.735, 0.735}
var lensR = 0.125
var lensRing = 0.045
var handle = [2][2]float64{{0.825, 0.825}, {0.905, 0.905}}
var handleW = 0.048

func segDist(px, py float64, a, b [2]float64) float64 {
	vx, vy := b[0]-a[0], b[1]-a[1]
	wx, wy := px-a[0], py-a[1]
	c1 := vx*wx + vy*wy
	if c1 <= 0 {
		return math.Hypot(wx, wy)
	}
	c2 := vx*vx + vy*vy
	if c2 <= c1 {
		return math.Hypot(px-b[0], py-b[1])
	}
	t := c1 / c2
	return math.Hypot(px-(a[0]+t*vx), py-(a[1]+t*vy))
}

// render 以 size×size 渲染指定主题图标（内部 4× 超采样抗锯齿）
func render(size int, th theme) *image.RGBA {
	S := size * 4
	img := image.NewRGBA(image.Rect(0, 0, S, S))
	radius := 0.20
	stroke := 0.098
	for y := 0; y < S; y++ {
		for x := 0; x < S; x++ {
			u, v := (float64(x)+0.5)/float64(S), (float64(y)+0.5)/float64(S)
			// 圆角方块内部判定
			cx, cy := math.Max(radius, math.Min(u, 1-radius)), math.Max(radius, math.Min(v, 1-radius))
			insideRect := u >= 0 && u <= 1 && v >= 0 && v <= 1 && math.Hypot(u-cx, v-cy) <= radius
			if !insideRect {
				continue
			}
			// 斜向光泽（左上→右下对角打光 + 过渡带），产生立体感
			t := ((1 - u) + v) / 2 // 0=左上受光面，1=右下背光面
			col := th.main
			switch {
			case t <= 0.40:
				col = th.light
			case t < 0.62:
				k := (t - 0.40) / 0.22 // 0→1 线性过渡
				k = k * k * (3 - 2*k)  // smoothstep
				col = blend(th.light, th.main, k)
			}
			img.Set(x, y, col)

			// 放大镜：柄 + 白色镜片（圆盘），镜片内绘制主题色地球仪经纬线
			if distSeg(u, v, handle[0], handle[1]) <= handleW/2 {
				img.Set(x, y, white)
			}
			dl := math.Hypot(u-lensC[0], v-lensC[1])
			if dl <= lensR+lensRing/2 {
				img.Set(x, y, white)
			}
			if dl < lensR-lensRing/2 {
				// 地球仪：外圈圆 + 赤道横线 + 纵向经线椭圆，主题色
				const gw = 0.013 // 线宽
				gr := (lensR - lensRing/2) * 0.62
				if math.Abs(dl-gr) <= gw {
					img.Set(x, y, th.main)
				}
				if math.Abs(v-lensC[1]) <= gw && math.Abs(u-lensC[0]) <= gr {
					img.Set(x, y, th.main)
				}
				// 经线：把 x 相对圆心压缩成椭圆后判定距离
				ecu := (u - lensC[0]) / 0.45
				ell := math.Hypot(ecu, v-lensC[1])
				if math.Abs(ell-gr) <= gw && math.Abs(u-lensC[0]) <= gr*0.45 {
					img.Set(x, y, th.main)
				}
			}

			// 白色 W：到 polyline 各段的距离
			dmin := 1e9
			for i := 0; i+1 < len(wpts); i++ {
				dmin = math.Min(dmin, segDist(u, v, wpts[i], wpts[i+1]))
			}
			if dmin <= stroke/2 {
				img.Set(x, y, white)
			}
		}
	}
	// 4× 盒式降采样
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b, a, n int
			for dy := 0; dy < 4; dy++ {
				for dx := 0; dx < 4; dx++ {
					c := img.RGBAAt(x*4+dx, y*4+dy)
					r, g, b, a = r+int(c.R), g+int(c.G), b+int(c.B), a+int(c.A)
					n++
				}
			}
			out.SetRGBA(x, y, color.RGBA{R: uint8(r / n), G: uint8(g / n), B: uint8(b / n), A: uint8(a / n)})
		}
	}
	return out
}

// blend 线性插值两个颜色（k=0 取 a，k=1 取 b）
func blend(a, b color.RGBA, k float64) color.RGBA {
	mix := func(x, y uint8) uint8 {
		return uint8(math.Round(float64(x)*(1-k) + float64(y)*k))
	}
	return color.RGBA{R: mix(a.R, b.R), G: mix(a.G, b.G), B: mix(a.B, b.B), A: 0xff}
}

// distSeg 计算点到线段的距离（放大镜柄）
func distSeg(px, py float64, a, b [2]float64) float64 {
	return segDist(px, py, a, b)
}

func pngBytes(size int, th theme) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, render(size, th)); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func buildICO(sizes []int, th theme) []byte {
	var pngs [][]byte
	for _, s := range sizes {
		pngs = append(pngs, pngBytes(s, th))
	}
	var out bytes.Buffer
	header := make([]byte, 6)
	binary.LittleEndian.PutUint16(header[0:], 0)
	binary.LittleEndian.PutUint16(header[2:], 1)
	binary.LittleEndian.PutUint16(header[4:], uint16(len(sizes)))
	out.Write(header)
	offset := 6 + 16*len(sizes)
	for i, s := range sizes {
		e := make([]byte, 16)
		if s >= 256 {
			e[0] = 0
		} else {
			e[0] = uint8(s)
		}
		e[1] = e[0]
		binary.LittleEndian.PutUint16(e[4:], 1)  // planes
		binary.LittleEndian.PutUint16(e[6:], 32) // bpp
		binary.LittleEndian.PutUint32(e[8:], uint32(len(pngs[i])))
		binary.LittleEndian.PutUint32(e[12:], uint32(offset))
		out.Write(e)
		offset += len(pngs[i])
	}
	for _, p := range pngs {
		out.Write(p)
	}
	return out.Bytes()
}

func main() {
	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	for name, th := range themes {
		ico := buildICO(sizes, th)
		icoPath := filepath.Join("cmd", "assets", "app-"+name+".ico")
		if err := os.WriteFile(icoPath, ico, 0o644); err != nil {
			panic(err)
		}
		logoPath := filepath.Join("pkg", "dashboard", "web", "logo-"+name+".png")
		if err := os.WriteFile(logoPath, pngBytes(256, th), 0o644); err != nil {
			panic(err)
		}
		fmt.Println("generated", icoPath, "+", logoPath)
	}
}
