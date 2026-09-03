package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"

	"github.com/fogleman/gg"
	qrcode "github.com/skip2/go-qrcode"
)

// GenerateCardImage تقوم بتوليد صورة الدعوة كاملاً لكل مدعو
func GenerateCardImage(guestName string, companions int, qrContent string) ([]byte, error) {
	// 1. أبعاد كارت الدعوة (مثلاً 600x1000)
	const width = 600
	const height = 1000

	dc := gg.NewContext(width, height)

	// 2. رسم الخلفية الكحلية (أو يمكنك تحميل صورة خلفية جاهزة بواسطة gg.LoadImage)
	dc.SetColor(color.RGBA{R: 11, G: 26, B: 52, A: 255}) // خلفية كحلي داكن
	dc.Clear()

	// 3. كتابة العناوين والنصوص ثابتة
	dc.SetColor(color.White)
	
	// تحميل خط يدعم العربية (مهم جداً وجود ملف خط عربى ttf مثل Cairo أو Amiri)
	if err := dc.LoadFontFace("fonts/Cairo-Bold.ttf", 28); err != nil {
		// في حال عدم وجود الخط، يفضل معالجة الخط لتفادي المشاكل
	}

	// كتابة النصوص المتروكة في المنتصف
	dc.DrawStringAnchored("دعوة", width/2, 180, 0.5, 0.5)
	
	if err := dc.LoadFontFace("fonts/Cairo-Regular.ttf", 18); err == nil {
		dc.DrawStringAnchored("لخريجي وخريجات جامعة جدة لعام 2026", width/2, 230, 0.5, 0.5)
	}

	if err := dc.LoadFontFace("fonts/Cairo-Bold.ttf", 22); err == nil {
		dc.DrawStringAnchored("لحضور حفل التخرج", width/2, 280, 0.5, 0.5)
	}

	// 4. كتابة اسم المدعو وعدد المرافقين ديناميكياً
	dc.SetColor(color.RGBA{R: 212, G: 225, B: 248, A: 255}) // لون أزرق فاتح/أبيض
	if err := dc.LoadFontFace("fonts/Cairo-Bold.ttf", 26); err == nil {
		dc.DrawStringAnchored(guestName, width/2, 380, 0.5, 0.5)
	}

	if err := dc.LoadFontFace("fonts/Cairo-Regular.ttf", 16); err == nil {
		compText := fmt.Sprintf("المرافقون: %d", companions)
		dc.DrawStringAnchored(compText, width/2, 420, 0.5, 0.5)
	}

	// 5. توليد الـ QR Code ودمجه داخل الصورة
	qrPNG, err := qrcode.Encode(qrContent, qrcode.Medium, 220)
	if err != nil {
		return nil, err
	}

	qrImg, _, err := image.Decode(bytes.NewReader(qrPNG))
	if err != nil {
		return nil, err
	}

	// رسم مربع أبيض حول الباركود
	dc.SetColor(color.White)
	dc.DrawRoundedRectangle(width/2-120, 480, 240, 240, 15)
	dc.Fill()

	// رسم صورة الباركود في المنتصف
	dc.DrawImage(qrImg, width/2-110, 490)

	// نص زير الباركود
	dc.SetColor(color.RGBA{R: 200, G: 210, B: 225, A: 255})
	if err := dc.LoadFontFace("fonts/Cairo-Regular.ttf", 14); err == nil {
		dc.DrawStringAnchored("أظهر هذا الباركود عند الدخول", width/2, 750, 0.5, 0.5)
	}

	// 6. استخراج الصورة كـ Bytes (PNG)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dc.Image()); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
