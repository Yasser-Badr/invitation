package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/skip2/go-qrcode"
	"github.com/xuri/excelize/v2"
)

func GradCreateGuest(c *gin.Context) {
	var input struct {
		Name       string `json:"name" binding:"required"`
		Phone      string `json:"phone" binding:"required"`
		Companions int    `json:"companions"`
		Gender     string `json:"gender"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	gender := strings.ToLower(strings.TrimSpace(input.Gender))
	if gender != "female" {
		gender = "male"
	}
	token := uuid.New().String()
	g := GradGuest{
		Name:       input.Name,
		Phone:      input.Phone,
		Companions: input.Companions,
		Gender:     gender,
		Token:      token,
	}
	// نولد QR فور الإضافة (مفيش تأكيد)
	baseURL := getAppBaseURL()
	verifyURL := fmt.Sprintf("%s/grad/verify/%s", baseURL, token)
	qrPath := fmt.Sprintf("./public/grad_qrcodes/%s.png", token)
	_ = os.MkdirAll("./public/grad_qrcodes", 0o755)
	if err := qrcode.WriteFile(verifyURL, qrcode.Medium, 256, qrPath); err == nil {
		g.QRImageURL = "/public/grad_qrcodes/" + token + ".png"
	}

	if err := GradDB.Create(&g).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "رقم الهاتف مسجل بالفعل"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "تم الإضافة", "data": g})
}

func GradRenderDashboard(c *gin.Context) {
	var guests []GradGuest
	GradDB.Find(&guests)
	var sent, checked int64
	GradDB.Model(&GradGuest{}).Where("invite_sent = ?", true).Count(&sent)
	GradDB.Model(&GradGuest{}).Where("checked_in = ?", true).Count(&checked)
	c.HTML(http.StatusOK, "grad_dashboard.html", gin.H{
		"Guests":         guests,
		"Total":          len(guests),
		"InviteSent":     sent,
		"NotSent":        int64(len(guests)) - sent,
		"CheckedIn":      checked,
		"Role":           getAdminRole(c),
	})
}

func GradRenderInvitePage(c *gin.Context) {
	token := c.Param("token")
	var g GradGuest
	if err := GradDB.Where("token = ?", token).First(&g).Error; err != nil {
		c.String(http.StatusNotFound, "الدعوة غير صالحة")
		return
	}
	settings := getGradSettings()
	c.HTML(http.StatusOK, "grad_invite.html", gin.H{
		"Guest":    g,
		"Settings": settings,
	})
}

// نص الرسالة الشخصية
func buildGradInviteMessage(g *GradGuest, s GradSettings, inviteLink string) string {
	companions := "بدون مرافقين"
	if g.Companions > 0 {
		companions = fmt.Sprintf("%d مرافق", g.Companions)
	}

	msg := fmt.Sprintf(
		"يا هلا فيك يا *%s* 🎓\n\n"+
			"%s\n"+
			"*%s*\n\n"+
			"👥 %s\n\n"+
			"🎫 باركود الحضور مرفق مع الرسالة\n"+
			"أو من خلال الرابط:\n%s\n",
		g.Name,
		s.EventSubtitle,
		s.MainLine,
		companions,
		inviteLink,
	)

	if s.DateText != "" {
		msg += "\n📅 " + s.DateText
	}
	if s.LocationName != "" {
		msg += "\n📍 " + s.LocationName
	}
	if s.FooterNote != "" {
		msg += "\n\n" + s.FooterNote
	}
	return msg
}

// إرسال دعوة لخريج واحد (تصميم + نص + باركود)
func sendGradInviteToGuest(g *GradGuest, baseURL string) error {
	if strings.TrimSpace(g.Phone) == "" {
		return fmt.Errorf("رقم فارغ")
	}
	if WAClient == nil || !WAClient.IsConnected() {
		return fmt.Errorf("whatsmeow غير متصل")
	}

	s := getGradSettings()
	inviteLink := fmt.Sprintf("%s/grad/invite/%s", strings.TrimRight(baseURL, "/"), g.Token)
	msg := buildGradInviteMessage(g, s, inviteLink)

	// 1) صورة الخلفية/التصميم إن وجدت
	if s.BackgroundURL != "" {
		path := "." + s.BackgroundURL
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			_ = SendWAImage(g.Phone, data, s.EventTitle+" — "+s.MainLine)
			time.Sleep(600 * time.Millisecond)
		}
	}

	// 2) نص الرسالة
	if err := SendWAMessage(g.Phone, msg); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)

	// 3) الباركود الشخصي
	if g.QRImageURL != "" {
		path := "." + g.QRImageURL
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			caption := fmt.Sprintf("باركود الحضور — %s\nأظهره عند الدخول 🎫", g.Name)
			if err := SendWAImage(g.Phone, data, caption); err != nil {
				return err
			}
		}
	}

	now := kuwaitNow()
	_ = GradDB.Model(&GradGuest{}).Where("id = ?", g.ID).Updates(map[string]interface{}{
		"invite_sent":    true,
		"invite_sent_at": now,
	})
	return nil
}

// بث جماعي مع concurrency آمن
func GradBroadcastHandler(c *gin.Context) {
	if WAClient == nil || !WAClient.IsConnected() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "whatsmeow غير متصل — امسح QR أولاً من لوحة الزفاف"})
		return
	}

	idsStr := c.PostForm("guest_ids")
	selectedOnly := c.PostForm("selected_only") == "1"

	var guests []GradGuest
	if selectedOnly || strings.TrimSpace(idsStr) != "" {
		parts := strings.Split(idsStr, ",")
		var ids []uint
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			var id uint
			if _, err := fmt.Sscanf(p, "%d", &id); err == nil && id > 0 {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "لم يتم تحديد خريجين"})
			return
		}
		GradDB.Where("id IN ?", ids).Find(&guests)
	} else {
		// الافتراضي: اللي لسه ما اتبعتلهمش
		GradDB.Where("invite_sent = ?", false).Find(&guests)
	}

	if len(guests) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "لا يوجد خريجين للإرسال"})
		return
	}

	scheme := "http"
	if c.Request.TLS != nil || c.Request.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	baseURL := fmt.Sprintf("%s://%s", scheme, c.Request.Host)
	if v := os.Getenv("APP_BASE_URL"); v != "" {
		baseURL = strings.TrimRight(v, "/")
	}

	type resultItem struct {
		ID    uint   `json:"id"`
		Name  string `json:"name"`
		Phone string `json:"phone"`
		Error string `json:"error,omitempty"`
	}

	var (
		successList []resultItem
		failList    []resultItem
		mu          sync.Mutex
		wg          sync.WaitGroup
	)

	const maxWorkers = 4
	sem := make(chan struct{}, maxWorkers)

	for _, guest := range guests {
		wg.Add(1)
		sem <- struct{}{}
		go func(g GradGuest) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					mu.Lock()
					failList = append(failList, resultItem{
						ID: g.ID, Name: g.Name, Phone: g.Phone,
						Error: fmt.Sprintf("panic: %v", r),
					})
					mu.Unlock()
				}
			}()

			err := sendGradInviteToGuest(&g, baseURL)
			mu.Lock()
			if err != nil {
				failList = append(failList, resultItem{
					ID: g.ID, Name: g.Name, Phone: g.Phone, Error: err.Error(),
				})
				fmt.Printf("❌ تخرج فشل %s: %v\n", g.Name, err)
			} else {
				successList = append(successList, resultItem{
					ID: g.ID, Name: g.Name, Phone: g.Phone,
				})
				fmt.Printf("✅ تخرج نجح %s\n", g.Name)
			}
			mu.Unlock()
			time.Sleep(500 * time.Millisecond)
		}(guest)
	}
	wg.Wait()

	c.JSON(http.StatusOK, gin.H{
		"message":       fmt.Sprintf("تخرج: %d نجح، %d فشل", len(successList), len(failList)),
		"success_count": len(successList),
		"fail_count":    len(failList),
		"success_list":  successList,
		"fail_list":     failList,
		"via":           "whatsmeow",
	})
}

func GradExportExcel(c *gin.Context) {
	var guests []GradGuest
	GradDB.Order("id asc").Find(&guests)

	f := excelize.NewFile()
	sheet := "الخريجون"
	f.SetSheetName("Sheet1", sheet)

	headers := []string{"م", "الاسم", "الهاتف", "الجنس", "المرافقين", "تم الإرسال", "الدخول", "وقت الدخول"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, h)
	}
	style, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"1A365D"}, Pattern: 1},
	})
	f.SetCellStyle(sheet, "A1", "H1", style)

	for i, g := range guests {
		row := i + 2
		gender := "ذكر"
		if g.Gender == "female" {
			gender = "أنثى"
		}
		sent, checked, checkedAt := "لا", "لا", "—"
		if g.InviteSent {
			sent = "نعم"
		}
		if g.CheckedIn {
			checked = "نعم"
			if g.CheckedInAt != nil {
				checkedAt = formatKuwait(*g.CheckedInAt)
			}
		}
		f.SetCellValue(sheet, fmt.Sprintf("A%d", row), g.ID)
		f.SetCellValue(sheet, fmt.Sprintf("B%d", row), g.Name)
		f.SetCellValue(sheet, fmt.Sprintf("C%d", row), g.Phone)
		f.SetCellValue(sheet, fmt.Sprintf("D%d", row), gender)
		f.SetCellValue(sheet, fmt.Sprintf("E%d", row), g.Companions)
		f.SetCellValue(sheet, fmt.Sprintf("F%d", row), sent)
		f.SetCellValue(sheet, fmt.Sprintf("G%d", row), checked)
		f.SetCellValue(sheet, fmt.Sprintf("H%d", row), checkedAt)
	}

	filename := fmt.Sprintf("graduation_export_%s.xlsx", time.Now().Format("2006-01-02_15-04"))
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	_ = f.Write(c.Writer)
}

func GradDeleteGuest(c *gin.Context) {
	id := c.Param("id")
	var g GradGuest
	if err := GradDB.Unscoped().First(&g, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "غير موجود"})
		return
	}
	if g.QRImageURL != "" {
		_ = os.Remove("." + g.QRImageURL)
	}
	GradDB.Unscoped().Delete(&g)
	c.JSON(http.StatusOK, gin.H{"message": "تم الحذف"})
}

func GradUpdateGuest(c *gin.Context) {
	id := c.Param("id")
	var input struct {
		Name       string `json:"name" binding:"required"`
		Phone      string `json:"phone" binding:"required"`
		Companions int    `json:"companions"`
		Gender     string `json:"gender"`
		CheckedIn  bool   `json:"checked_in"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var g GradGuest
	if err := GradDB.First(&g, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "غير موجود"})
		return
	}
	g.Name = strings.TrimSpace(input.Name)
	g.Phone = strings.TrimSpace(input.Phone)
	g.Companions = input.Companions
	if strings.ToLower(input.Gender) == "female" {
		g.Gender = "female"
	} else {
		g.Gender = "male"
	}
	if input.CheckedIn && !g.CheckedIn {
		now := kuwaitNow()
		g.CheckedIn = true
		g.CheckedInAt = &now
	} else if !input.CheckedIn {
		g.CheckedIn = false
		g.CheckedInAt = nil
	}
	if err := GradDB.Save(&g).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "فشل الحفظ"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "تم التعديل", "guest": g})
}

func GradRenderSettings(c *gin.Context) {
	c.HTML(http.StatusOK, "grad_settings.html", gin.H{
		"Settings": getGradSettings(),
		"success":  c.Query("success"),
	})
}

func GradUpdateSettings(c *gin.Context) {
	var s GradSettings
	GradDB.First(&s)

	s.EventTitle = c.PostForm("event_title")
	s.EventSubtitle = c.PostForm("event_subtitle")
	s.MainLine = c.PostForm("main_line")
	s.SubLine = c.PostForm("sub_line")
	s.DateText = c.PostForm("date_text")
	s.LocationName = c.PostForm("location_name")
	s.LocationAddress = c.PostForm("location_address")
	s.MapsURL = c.PostForm("maps_url")
	s.FooterNote = c.PostForm("footer_note")
	s.PrimaryColor = c.PostForm("primary_color")
	s.SecondaryColor = c.PostForm("secondary_color")
	s.EventDate = strings.TrimSpace(c.PostForm("event_date"))
	s.EventTime = strings.TrimSpace(c.PostForm("event_time"))

	_ = os.MkdirAll("./public/grad_uploads", 0o755)

	if file, err := c.FormFile("logo"); err == nil {
		if s.LogoURL != "" {
			_ = os.Remove("." + s.LogoURL)
		}
		name := "logo_" + uuid.New().String() + filepath.Ext(file.Filename)
		_ = c.SaveUploadedFile(file, "./public/grad_uploads/"+name)
		s.LogoURL = "/public/grad_uploads/" + name
	}
	if file, err := c.FormFile("background"); err == nil {
		if s.BackgroundURL != "" {
			_ = os.Remove("." + s.BackgroundURL)
		}
		name := "bg_" + uuid.New().String() + filepath.Ext(file.Filename)
		_ = c.SaveUploadedFile(file, "./public/grad_uploads/"+name)
		s.BackgroundURL = "/public/grad_uploads/" + name
	}
	if c.PostForm("remove_logo") == "1" {
		if s.LogoURL != "" {
			_ = os.Remove("." + s.LogoURL)
		}
		s.LogoURL = ""
	}
	if c.PostForm("remove_background") == "1" {
		if s.BackgroundURL != "" {
			_ = os.Remove("." + s.BackgroundURL)
		}
		s.BackgroundURL = ""
	}

	GradDB.Save(&s)
	c.Redirect(http.StatusFound, "/grad/admin/settings?success=1")
}

func GradImportExcel(c *gin.Context) {
	file, err := c.FormFile("excel")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "يجب رفع ملف Excel"})
		return
	}
	tempPath := "./temp_grad_import.xlsx"
	if err := c.SaveUploadedFile(file, tempPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "فشل حفظ الملف"})
		return
	}
	defer os.Remove(tempPath)

	f, err := excelize.OpenFile(tempPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ملف غير صالح"})
		return
	}
	defer f.Close()

	rows, err := f.GetRows(f.GetSheetName(0))
	if err != nil || len(rows) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "لا توجد بيانات"})
		return
	}

	success, fail := 0, 0
	baseURL := getAppBaseURL()
	_ = os.MkdirAll("./public/grad_qrcodes", 0o755)

	for i, row := range rows {
		if i == 0 || len(row) < 2 {
			continue
		}
		name := strings.TrimSpace(row[0])
		phone := strings.TrimSpace(row[1])
		companions := 0
		if len(row) >= 3 {
			companions, _ = strconv.Atoi(strings.TrimSpace(row[2]))
		}
		gender := "male"
		if len(row) >= 4 {
			g := strings.ToLower(strings.TrimSpace(row[3]))
			if g == "female" || g == "f" || g == "انثى" || g == "أنثى" {
				gender = "female"
			}
		}
		if name == "" || phone == "" {
			fail++
			continue
		}
		token := uuid.New().String()
		guest := GradGuest{Name: name, Phone: phone, Companions: companions, Gender: gender, Token: token}
		verifyURL := fmt.Sprintf("%s/grad/verify/%s", baseURL, token)
		qrFile := token + ".png"
		if err := qrcode.WriteFile(verifyURL, qrcode.Medium, 256, "./public/grad_qrcodes/"+qrFile); err == nil {
			guest.QRImageURL = "/public/grad_qrcodes/" + qrFile
		}
		if err := GradDB.Create(&guest).Error; err != nil {
			fail++
			continue
		}
		success++
	}
	c.JSON(http.StatusOK, gin.H{"message": "تم الاستيراد", "success_count": success, "fail_count": fail})
}

func GradAPIVerify(c *gin.Context) {
	token := c.Param("token")
	var g GradGuest
	if err := GradDB.Where("token = ?", token).First(&g).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "باركود غير مسجل"})
		return
	}
	if g.CheckedIn {
		at := ""
		if g.CheckedInAt != nil {
			at = formatKuwait(*g.CheckedInAt)
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true, "already_checked_in": true,
			"name": g.Name, "companions": g.Companions, "checked_in_at": at,
			"message": "تم تسجيل الدخول مسبقًا",
		})
		return
	}
	now := kuwaitNow()
	g.CheckedIn = true
	g.CheckedInAt = &now
	GradDB.Save(&g)
	c.JSON(http.StatusOK, gin.H{
		"success": true, "already_checked_in": false,
		"name": g.Name, "companions": g.Companions,
		"message": "تم تسجيل الحضور بنجاح",
	})
}

func GradRenderVerifyPage(c *gin.Context) {
	token := c.Param("token")
	var g GradGuest
	if err := GradDB.Where("token = ?", token).First(&g).Error; err != nil {
		c.HTML(http.StatusOK, "grad_verify.html", gin.H{"Error": "باركود غير مسجل", "Name": ""})
		return
	}
	c.HTML(http.StatusOK, "grad/grad_verify.html", gin.H{
		"Error": "", "Name": g.Name, "Companions": g.Companions, "CheckedIn": g.CheckedIn,
	})
}