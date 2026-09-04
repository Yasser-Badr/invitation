package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"strings"

	"github.com/skip2/go-qrcode"
	"image/png"
)

// bgPath اختياري — لو فاضي تستخدم لون أخضر غامق أو خلفية الإعدادات
func generateWeddingInvitePNG(g *Guest, bgPath string) ([]byte, error) {
	const W, H = 600, 900
	img := image.NewRGBA(image.Rect(0, 0, W, H))

	// لون افتراضي قريب لهوية الزفاف
	bg := color.RGBA{R: 0x1b, G: 0x43, B: 0x32, A: 0xff}
	draw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)

	if strings.TrimSpace(bgPath) != "" {
		if f, err := os.Open(bgPath); err == nil {
			if decoded, _, err2 := image.Decode(f); err2 == nil {
				draw.Draw(img, img.Bounds(), resizeWeddingBG(decoded, W, H), image.Point{}, draw.Src)
			}
			f.Close()
		}
	} else {
		// خلفية من إعدادات الدعوة لو موجودة
		s := getSettings()
		if s.BackgroundURL != "" {
			if f, err := os.Open("." + s.BackgroundURL); err == nil {
				if decoded, _, err2 := image.Decode(f); err2 == nil {
					draw.Draw(img, img.Bounds(), resizeWeddingBG(decoded, W, H), image.Point{}, draw.Src)
				}
				f.Close()
			}
		}
	}

	// باركود التحقق (صفحة verify بتاعة الزفاف)
	const qrSize = 260
	base := strings.TrimRight(getAppBaseURL(), "/")
	qrContent := fmt.Sprintf("%s/verify/%s", base, g.Token)

	qrBytes, err := qrcode.Encode(qrContent, qrcode.Medium, qrSize)
	if err != nil {
		return nil, err
	}
	qrImg, err := png.Decode(bytes.NewReader(qrBytes))
	if err != nil {
		return nil, err
	}

	x := (W - qrSize) / 2
	y := H/2 - qrSize/2 + 40
	draw.Draw(img, image.Rect(x, y, x+qrSize, y+qrSize), qrImg, image.Point{}, draw.Src)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func resizeWeddingBG(src image.Image, w, h int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	sr := src.Bounds()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sx := sr.Min.X + x*sr.Dx()/w
			sy := sr.Min.Y + y*sr.Dy()/h
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func saveWeddingCardPublic(g *Guest, bgPath string) (string, error) {
	data, err := generateWeddingInvitePNG(g, bgPath)
	if err != nil {
		return "", err
	}
	_ = os.MkdirAll("./public/wedding_cards", 0o755)
	name := fmt.Sprintf("%s.png", g.Token)
	path := "./public/wedding_cards/" + name
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}
	base := strings.TrimRight(getAppBaseURL(), "/")
	return base + "/public/wedding_cards/" + name, nil
}