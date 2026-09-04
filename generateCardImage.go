package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"strings"

	"github.com/skip2/go-qrcode"
)

// bgPath اختياري: مسار صورة خلفية من الرفع. لو فاضي نستخدم لون كحلي فقط.
func generateGradInvitePNG(g *GradGuest, bgPath string) ([]byte, error) {
	const W, H = 600, 900

	img := image.NewRGBA(image.Rect(0, 0, W, H))

	// خلفية افتراضية كحلي
	bg := color.RGBA{R: 0x0a, G: 0x1f, B: 0x3a, A: 0xff}
	draw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)

	// خلفية من الملف المرفوع (أولوية)
	if strings.TrimSpace(bgPath) != "" {
		if f, err := os.Open(bgPath); err == nil {
			if decoded, _, err2 := image.Decode(f); err2 == nil {
				// نمدّد الخلفية على حجم البطاقة
				draw.Draw(img, img.Bounds(), resizeImage(decoded, W, H), image.Point{}, draw.Src)
			}
			f.Close()
		}
	}

	// باركود أصغر — بدون إطار أبيض إضافي
	const qrSize = 200 // كان 320 — صغّر أكثر لو حابب (160 / 140)
	base := strings.TrimRight(getAppBaseURL(), "/")
	key := os.Getenv("GRAD_SCAN_KEY")
	qrContent := fmt.Sprintf("%s/grad/public-verify/%s?key=%s", base, g.Token, key)

	qrBytes, err := qrcode.Encode(qrContent, qrcode.Medium, qrSize)
	if err != nil {
		return nil, err
	}
	qrImg, err := png.Decode(bytes.NewReader(qrBytes))
	if err != nil {
		return nil, err
	}

	// في منتصف البطاقة تقريبًا (عدّل y لو التصميم محتاج الباركود تحت أكتر)
	x := (W - qrSize) / 2
	y := H/2 - qrSize/2 + 40

	draw.Draw(img, image.Rect(x, y, x+qrSize, y+qrSize), qrImg, image.Point{}, draw.Src)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// تغيير بسيط لحجم الخلفية لتطابق البطاقة
func resizeImage(src image.Image, w, h int) image.Image {
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