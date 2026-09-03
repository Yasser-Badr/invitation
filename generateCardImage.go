package main

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"strings"

	"github.com/fogleman/gg"
	"github.com/skip2/go-qrcode"
)

// عكس النص العربي عشان gg ترسمه بالاتجاه الصحيح
func rtl(s string) string {
	r := []rune(strings.TrimSpace(s))
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

func generateGradInvitePNG(g *GradGuest, s GradSettings) ([]byte, error) {
	const W, H = 600, 920
	dc := gg.NewContext(W, H)

	// خلفية كحلي
	dc.SetRGB(0.04, 0.12, 0.23)
	dc.Clear()

	if s.BackgroundURL != "" {
		if img, err := gg.LoadImage("." + s.BackgroundURL); err == nil {
			dc.DrawImage(img, 0, 0)
		}
	}

	fontPath := "fonts/NotoNaskhArabic-Regular.ttf"
	if err := dc.LoadFontFace(fontPath, 28); err != nil {
		_ = dc.LoadFontFace("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 24)
	}

	// عنوان
	title := s.EventTitle
	if title == "" {
		title = "دعوة"
	}
	_ = dc.LoadFontFace(fontPath, 36)
	dc.SetRGB(1, 1, 1)
	dc.DrawStringAnchored(rtl(title), W/2, 110, 0.5, 0.5)

	// سطر فرعي
	_ = dc.LoadFontFace(fontPath, 18)
	dc.SetRGB(0.75, 0.82, 0.90)
	if s.EventSubtitle != "" {
		dc.DrawStringAnchored(rtl(s.EventSubtitle), W/2, 165, 0.5, 0.5)
	}

	// السطر الرئيسي
	_ = dc.LoadFontFace(fontPath, 24)
	dc.SetRGB(1, 1, 1)
	if s.MainLine != "" {
		dc.DrawStringAnchored(rtl(s.MainLine), W/2, 220, 0.5, 0.5)
	}

	// الاسم
	_ = dc.LoadFontFace(fontPath, 28)
	dc.SetRGB(0.55, 0.75, 1)
	dc.DrawStringAnchored(rtl(g.Name), W/2, 290, 0.5, 0.5)

	// المرافقين
	comp := "بدون مرافقين"
	if g.Companions > 0 {
		comp = fmt.Sprintf("المرافقون: %d", g.Companions)
	}
	_ = dc.LoadFontFace(fontPath, 18)
	dc.SetRGB(0.7, 0.78, 0.85)
	dc.DrawStringAnchored(rtl(comp), W/2, 335, 0.5, 0.5)

	// باركود التحقق
	base := strings.TrimRight(getAppBaseURL(), "/")
	key := os.Getenv("GRAD_SCAN_KEY")
	qrContent := fmt.Sprintf("%s/grad/public-verify/%s?key=%s", base, g.Token, key)
	qrBytes, err := qrcode.Encode(qrContent, qrcode.Medium, 280)
	if err != nil {
		return nil, err
	}
	qrImg, err := png.Decode(bytes.NewReader(qrBytes))
	if err != nil {
		return nil, err
	}

	boxX, boxY, boxS := 160, 390, 280
	dc.SetRGB(1, 1, 1)
	dc.DrawRoundedRectangle(float64(boxX-12), float64(boxY-12), float64(boxS+24), float64(boxS+24), 16)
	dc.Fill()
	dc.DrawImage(qrImg, boxX, boxY)

	_ = dc.LoadFontFace(fontPath, 16)
	dc.SetRGB(0.65, 0.72, 0.80)
	dc.DrawStringAnchored(rtl("أظهر هذا الباركود عند الدخول"), W/2, 710, 0.5, 0.5)

	// تذييل بدون إيميلات
	if s.DateText != "" {
		_ = dc.LoadFontFace(fontPath, 14)
		dc.SetRGB(0.6, 0.68, 0.75)
		dc.DrawStringAnchored(rtl(s.DateText), W/2, 780, 0.5, 0.5)
	}
	if s.LocationName != "" {
		_ = dc.LoadFontFace(fontPath, 14)
		dc.DrawStringAnchored(rtl(s.LocationName), W/2, 810, 0.5, 0.5)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, dc.Image()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}