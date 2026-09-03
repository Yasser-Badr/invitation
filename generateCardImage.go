package main

import (
	"bytes"
	"fmt"
	//"image"
	"image/png"

	"github.com/fogleman/gg"
	"github.com/skip2/go-qrcode"
)

// توليد بطاقة دعوة شخصية (تصميم + اسم + مرافقين + باركود)
func generateGradInvitePNG(g *GradGuest, s GradSettings) ([]byte, error) {
	const W, H = 600, 920
	dc := gg.NewContext(W, H)

	// خلفية كحلي
	dc.SetRGB(0.04, 0.12, 0.23) // #0a1f3a
	dc.Clear()

	// لو في خلفية مرفوعة نستخدمها
	if s.BackgroundURL != "" {
		if img, err := gg.LoadImage("." + s.BackgroundURL); err == nil {
			dc.DrawImage(img, 0, 0)
		}
	}

	// خط عربي
	fontPath := "fonts/NotoNaskhArabic-Regular.ttf"
	if err := dc.LoadFontFace(fontPath, 28); err != nil {
		// fallback
		_ = dc.LoadFontFace("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 24)
	}

	dc.SetRGB(1, 1, 1)

	// عنوان
	title := s.EventTitle
	if title == "" {
		title = "دعوة"
	}
	dc.LoadFontFace(fontPath, 36)
	dc.DrawStringAnchored(title, W/2, 120, 0.5, 0.5)

	// سطر فرعي
	dc.LoadFontFace(fontPath, 20)
	dc.SetRGB(0.75, 0.82, 0.90)
	dc.DrawStringAnchored(s.EventSubtitle, W/2, 175, 0.5, 0.5)

	// السطر الرئيسي
	dc.LoadFontFace(fontPath, 26)
	dc.SetRGB(1, 1, 1)
	dc.DrawStringAnchored(s.MainLine, W/2, 230, 0.5, 0.5)

	// الاسم
	dc.LoadFontFace(fontPath, 30)
	dc.SetRGB(0.55, 0.75, 1)
	dc.DrawStringAnchored(g.Name, W/2, 300, 0.5, 0.5)

	// المرافقين
	comp := "بدون مرافقين"
	if g.Companions > 0 {
		comp = fmt.Sprintf("المرافقون: %d", g.Companions)
	}
	dc.LoadFontFace(fontPath, 20)
	dc.SetRGB(0.7, 0.78, 0.85)
	dc.DrawStringAnchored(comp, W/2, 345, 0.5, 0.5)

	// الباركود
	qrBytes, err := qrcode.Encode(
		fmt.Sprintf("%s/grad/public-verify/%s?key=%s",
			strings.TrimRight(getAppBaseURL(), "/"),
			g.Token,
			os.Getenv("GRAD_SCAN_KEY"),
		),
		qrcode.Medium,
		280,
	)
	if err != nil {
		return nil, err
	}
	qrImg, err := png.Decode(bytes.NewReader(qrBytes))
	if err != nil {
		return nil, err
	}

	// إطار أبيض للباركود
	boxX, boxY, boxS := 160, 400, 280
	dc.SetRGB(1, 1, 1)
	dc.DrawRoundedRectangle(float64(boxX-12), float64(boxY-12), float64(boxS+24), float64(boxS+24), 16)
	dc.Fill()
	dc.DrawImage(qrImg, boxX, boxY)

	// جملة تحت الباركود
	dc.LoadFontFace(fontPath, 18)
	dc.SetRGB(0.65, 0.72, 0.80)
	dc.DrawStringAnchored("أظهر هذا الباركود عند الدخول", W/2, 720, 0.5, 0.5)

	// التذييل
	if s.FooterNote != "" {
		dc.LoadFontFace(fontPath, 14)
		dc.SetRGB(0.55, 0.62, 0.70)
		dc.DrawStringAnchored(s.FooterNote, W/2, 860, 0.5, 0.5)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, dc.Image()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
