package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"strings"

	"github.com/skip2/go-qrcode"
)

// بطاقة نظيفة: خلفية كحلي + باركود فقط (بدون نص عربي على الصورة)
func generateGradInvitePNG(g *GradGuest, s GradSettings) ([]byte, error) {
	const W, H = 600, 900

	img := image.NewRGBA(image.Rect(0, 0, W, H))

	// خلفية كحلي
	bg := color.RGBA{R: 0x0a, G: 0x1f, B: 0x3a, A: 0xff}
	draw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)

	// لو في خلفية مرفوعة من الإعدادات
	if s.BackgroundURL != "" {
		if f, err := os.Open("." + s.BackgroundURL); err == nil {
			if decoded, _, err2 := image.Decode(f); err2 == nil {
				draw.Draw(img, img.Bounds(), decoded, image.Point{}, draw.Src)
			}
			f.Close()
		}
	}

	// باركود التحقق
	base := strings.TrimRight(getAppBaseURL(), "/")
	key := os.Getenv("GRAD_SCAN_KEY")
	qrContent := fmt.Sprintf("%s/grad/public-verify/%s?key=%s", base, g.Token, key)

	qrBytes, err := qrcode.Encode(qrContent, qrcode.Medium, 320)
	if err != nil {
		return nil, err
	}
	qrImg, err := png.Decode(bytes.NewReader(qrBytes))
	if err != nil {
		return nil, err
	}

	// إطار أبيض + الباركود في النص
	qrSize := 320
	x := (W - qrSize) / 2
	y := (H - qrSize) / 2

	pad := 18
	white := color.RGBA{255, 255, 255, 255}
	frame := image.Rect(x-pad, y-pad, x+qrSize+pad, y+qrSize+pad)
	draw.Draw(img, frame, &image.Uniform{white}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(x, y, x+qrSize, y+qrSize), qrImg, image.Point{}, draw.Src)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}