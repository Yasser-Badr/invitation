package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"time"
	"path/filepath"
	"github.com/joho/godotenv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/skip2/go-qrcode"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"strconv"
    "strings"
    "github.com/xuri/excelize/v2"
)

type Guest struct {
	gorm.Model
	Name        string
	Phone       string `gorm:"uniqueIndex"`
	Companions  int    `gorm:"default:0"`
	Gender      string `gorm:"default:'male'"` // male | female
	Token       string `gorm:"uniqueIndex"`
	QRImageURL  string
	Status      string `gorm:"default:'pending'"` // pending | confirmed | declined
	CheckedIn   bool   `gorm:"default:false"`
	CheckedInAt *time.Time // وقت الدخول الفعلي (null لو لسه مدخلش)
    InviteSent    bool       `gorm:"default:false"` // ← جديد
	InviteSentAt  *time.Time // ← جديد
}

type InvitationSettings struct {
	gorm.Model
	EventTitle      string
	EventSubtitle   string
	Person1         string
	Person2         string
	DateText        string
	LocationName    string
	LocationAddress string
	MapsURL         string
	FooterQuote     string
	PrimaryColor    string
	SecondaryColor  string

	// الصور الجديدة
	LogoURL         string // صورة اللوجو أو الزخرفة العلوية
	BackgroundURL   string // خلفية البطاقة (اختياري)
	IconLocationURL string // أيقونة الموقع
	IconDateURL     string // أيقونة التاريخ

    EventDate string // مثال: 2026-09-20  (YYYY-MM-DD)
    EventTime string // مثال: 19:00
    
}
type AdminUser struct {
	gorm.Model
	Username string `gorm:"uniqueIndex"`
	Password string
	Role     string // "manager" | "scanner" | "reception"
	Name     string
}

var DB *gorm.DB

func ConnectDB() {
	database, err := gorm.Open(sqlite.Open("wedding_test.db"), &gorm.Config{})
	if err != nil {
		log.Fatal("❌ فشل الاتصال بقاعدة البيانات:", err)
	}

    err = database.AutoMigrate(&Guest{}, &InvitationSettings{}, &AdminUser{})
	if err != nil {
		log.Fatal("❌ فشل عمل Migration:", err)
	}
	DB = database
    
    var userCount int64
    DB.Model(&AdminUser{}).Count(&userCount)
    if userCount == 0 {
	    DB.Create(&AdminUser{Username: "manager", Password: "Faisal@2026", Role: "manager", Name: "مدير النظام"})
	    DB.Create(&AdminUser{Username: "scan", Password: "Scan@123", Role: "scanner", Name: "موظف المسح"})
	    DB.Create(&AdminUser{Username: "reception", Password: "Rec@123", Role: "reception", Name: "الاستقبال"})
    }
	// إنشاء إعدادات افتراضية لو مفيش
	var count int64
	DB.Model(&InvitationSettings{}).Count(&count)
	if count == 0 {
		DB.Create(&InvitationSettings{
			EventTitle:      "دعوة",
			EventSubtitle:   "بكل الود والمحبة والتقدير\nنتشرف بدعوتكم لحضور حفل زفاف",
			Person1:         "عبدالله",
			Person2:         "عائشة",
			DateText:        "يوم الجمعة\n١٩ - ٠٥ - ١٤٤٣ هـ",
			LocationName:    "قاعة فرح",
			LocationAddress: "شارع الجامعة",
			MapsURL:         "https://maps.google.com/?q=شارع+الجامعة",
			FooterQuote:     "وبحضوركم يتم لنا الفرح والسرور",
			PrimaryColor:    "#6b7045",
			SecondaryColor:  "#9b705d",
			LogoURL:         "",
            BackgroundURL:   "",
            IconLocationURL: "",
            IconDateURL:     "",
		})
	}
}

type CreateGuestInput struct {
	Name       string `json:"name" binding:"required"`
	Phone      string `json:"phone" binding:"required"`
	Companions int    `json:"companions"`
	Gender     string `json:"gender"` // male | female
}


func CreateGuest(c *gin.Context) {
	var input CreateGuestInput

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	guestToken := uuid.New().String()
    gender := strings.ToLower(strings.TrimSpace(input.Gender))
    if gender != "female" {
        gender = "male"
      }
	newGuest := Guest{
		Name:       input.Name,
		Phone:      input.Phone,
		Companions: input.Companions,
		Gender:     gender,
		Token:      guestToken,
		QRImageURL: "",
		Status:     "pending",
	}

	if err := DB.Create(&newGuest).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "رقم الهاتف مسجل بالفعل!"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "تم الإضافة بنجاح",
		"data":    newGuest,
	})
}

type LoginInput struct {
	Phone string `json:"phone" binding:"required"`
}

func LoginByPhone(c *gin.Context) {
	var input LoginInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "رقم الهاتف مطلوب"})
		return
	}

	var guest Guest
	if err := DB.Where("phone = ?", input.Phone).First(&guest).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "الرقم غير مسجل لدينا"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": guest.Token})
}

func RenderInvitePage(c *gin.Context) {
	token := c.Param("token")
	var guest Guest

	if err := DB.Where("token = ?", token).First(&guest).Error; err != nil {
		c.String(http.StatusNotFound, "الدعوة غير صالحة")
		return
	}

    adminWA := adminWhatsAppURL()
    settings := getSettings()
	locQR := ensureLocationQR(settings.MapsURL)
	closed := isRSVPClosed()
	c.HTML(http.StatusOK, "invite.html", gin.H{
		"Name":          guest.Name,
		"Companions":    guest.Companions,
		"Token":         guest.Token,
		"Gender":        guest.Gender,
		"Settings":      settings,
		"LocationQRURL": locQR,
		"RSVPClosed":    closed,
		"ClosedMsg":     rsvpClosedMessage(),
		"AdminWhatsApp": adminWA,   // ← أضف السطر ده
	})
}

type RSVPInput struct {
	Token  string `json:"token" binding:"required"`
	Status string `json:"status" binding:"required"`
}

func UpdateRSVP(c *gin.Context) {
    if isRSVPClosed() {
		c.JSON(http.StatusForbidden, gin.H{"error": rsvpClosedMessage()})
		return
	}
	var input RSVPInput

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "بيانات غير صالحة"})
		return
	}

	var guest Guest
	if err := DB.Where("token = ?", input.Token).First(&guest).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "الضيف غير موجود"})
		return
	}

	if input.Status == "confirmed" && guest.QRImageURL == "" {
		// تم التعديل هنا ليلتقط الدومين الخاص بـ Google Cloud Run أو اللوكال هوست تلقائياً
		scheme := "http"
		if c.Request.TLS != nil || c.Request.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		host := c.Request.Host
		verifyURL := fmt.Sprintf("%s://%s/verify/%s", scheme, host, guest.Token)
		
		qrFileName := fmt.Sprintf("%s.png", guest.Token)
		qrFilePath := fmt.Sprintf("./public/qrcodes/%s", qrFileName)
		
		os.MkdirAll("./public/qrcodes", os.ModePerm)
		err := qrcode.WriteFile(verifyURL, qrcode.Medium, 256, qrFilePath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "فشل في توليد الباركود"})
			return
		}
		
		guest.QRImageURL = fmt.Sprintf("/public/qrcodes/%s", qrFileName)
	}

	guest.Status = input.Status
	DB.Save(&guest)

	c.JSON(http.StatusOK, gin.H{"message": "تم تحديث حالة الحضور"})
}

func RenderTicketPage(c *gin.Context) {
	token := c.Param("token")
	var guest Guest

	if err := DB.Where("token = ?", token).First(&guest).Error; err != nil {
		c.String(http.StatusNotFound, "البطاقة غير موجودة")
		return
	}

	if guest.Status != "confirmed" {
		c.String(http.StatusForbidden, "يجب تأكيد الحضور أولاً لإصدار بطاقة الدخول")
		return
	}

	settings := getSettings()

	c.HTML(http.StatusOK, "ticket.html", gin.H{
		"ID":         guest.ID,
		"Name":       guest.Name,
		"Phone":      guest.Phone,
		"Companions": guest.Companions,
		"QRImageURL": guest.QRImageURL,
		"Settings":   settings,
	})
}
// === دالة عرض لوحة التحكم المُحدثة ===
func RenderDashboard(c *gin.Context) {
	var allGuests []Guest
	var confirmedGuests []Guest
	var declinedGuests []Guest
	var checkedInCount int64

	DB.Find(&allGuests)
	DB.Where("status = ?", "confirmed").Find(&confirmedGuests)
	DB.Where("status = ?", "declined").Find(&declinedGuests)
	DB.Model(&Guest{}).Where("checked_in = ?", true).Count(&checkedInCount)

	total := len(allGuests)
	confirmed := len(confirmedGuests)
	declined := len(declinedGuests)
	pending := total - confirmed - declined

	attendanceRate := 0.0
	if confirmed > 0 {
		attendanceRate = float64(checkedInCount) / float64(confirmed) * 100
	}
	
    var inviteSentCount int64
    DB.Model(&Guest{}).Where("invite_sent = ?", true).Count(&inviteSentCount)

    notSentCount := int64(total) - inviteSentCount
	
	role := getAdminRole(c)

	c.HTML(http.StatusOK, "dashboard.html", gin.H{
		"AllGuests":       allGuests,
		"Confirmed":       confirmedGuests,
		"Declined":        declinedGuests,
		"TotalGuests":     total,
		"ConfirmedCount":  confirmed,
		"DeclinedCount":   declined,
		"PendingCount":    pending,
		"CheckedInCount":  checkedInCount,
		"AttendanceRate":  fmt.Sprintf("%.1f", attendanceRate),
		"Role":            role, // عشان نخفي أزرار حسب الصلاحية
		"InviteSentCount": inviteSentCount,
        "NotSentCount":    notSentCount,
	})
}

// === دالة الطباعة المنفصلة ===
func PrintReport(c *gin.Context) {
	status := c.Param("status")
	var guests []Guest
	var title string

	switch status {
case "confirmed":
		DB.Where("status = ?", "confirmed").Find(&guests)
		title = "قائمة مؤكدي الحضور"
	case "declined":
		DB.Where("status = ?", "declined").Find(&guests)
		title = "قائمة المعتذرين عن الحضور"
	case "all":
		DB.Find(&guests) // جلب جميع المدعوين
		title = "قائمة جميع المدعوين"
	default:
		c.String(http.StatusBadRequest, "طلب غير صالح")
		return
	}

	c.HTML(http.StatusOK, "print.html", gin.H{
		"Title":  title,
		"Guests": guests,
	})
}

// === دالة الحذف المحدثة (لحذف الباركود مع البيانات) ===
// === دالة الحذف المحدثة (لحذف نهائي Unscoped) ===
func DeleteGuestAdmin(c *gin.Context) {
	id := c.Param("id")
	var guest Guest

	// 1. البحث عن الضيف أولاً (نستخدم Unscoped للبحث حتى لو كان محذوفاً وهمياً)
	if err := DB.Unscoped().First(&guest, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "الضيف غير موجود"})
		return
	}

	// 2. حذف صورة الباركود من السيرفر إذا كانت موجودة
	if guest.QRImageURL != "" {
		filePath := "." + guest.QRImageURL
		os.Remove(filePath)
	}

	// 3. حذف الضيف نهائياً من قاعدة البيانات (Hard Delete)
	if err := DB.Unscoped().Delete(&guest).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "فشل الحذف من قاعدة البيانات"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "تم الحذف النهائي بنجاح"})
}
type UpdateGuestInput struct {
	Name       string `json:"name" binding:"required"`
	Phone      string `json:"phone" binding:"required"`
	Companions int    `json:"companions"`
	CheckedIn  bool   `json:"checked_in"`
	Status     string `json:"status"`
	Gender     string `json:"gender"`
}

func UpdateGuestAdmin(c *gin.Context) {
	id := c.Param("id")
	var input UpdateGuestInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var guest Guest
	if err := DB.First(&guest, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "الضيف غير موجود"})
		return
	}

	oldStatus := guest.Status
	newStatus := strings.TrimSpace(input.Status)
	if newStatus == "" {
		newStatus = oldStatus
	}
	if newStatus != "pending" && newStatus != "confirmed" && newStatus != "declined" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "حالة غير صالحة (pending / confirmed / declined)"})
		return
	}

	guest.Name = input.Name
	guest.Phone = input.Phone
	guest.Companions = input.Companions
	g := strings.ToLower(strings.TrimSpace(input.Gender))
	if g == "female" {
		guest.Gender = "female"
	} else if g == "male" {
		guest.Gender = "male"
	}
	guest.CheckedIn = input.CheckedIn

	// إلغاء الباركود عند الاعتذار أو الرجوع لقيد الانتظار
// إلغاء الباركود عند الاعتذار أو الرجوع لقيد الانتظار
	if newStatus == "declined" || newStatus == "pending" {
		if guest.QRImageURL != "" {
			_ = os.Remove("." + guest.QRImageURL)
			guest.QRImageURL = ""
		}
		// CheckedIn و CheckedInAt يتسابوا زي ما هما
	}

	// لو الأدمن رجّع "لم يدخل" يدوياً
	if !input.CheckedIn {
		guest.CheckedIn = false
		guest.CheckedInAt = nil
	} else if input.CheckedIn && !guest.CheckedIn {
		now := kuwaitNow()
		guest.CheckedIn = true
		guest.CheckedInAt = &now
	} else {
		guest.CheckedIn = input.CheckedIn
	}
	
	// تأكيد يدوي من الأدمن بدون باركود → توليد باركود
	if newStatus == "confirmed" && guest.QRImageURL == "" {
		scheme := "http"
		if c.Request.TLS != nil || c.Request.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		host := c.Request.Host
		verifyURL := fmt.Sprintf("%s://%s/verify/%s", scheme, host, guest.Token)

		qrFileName := fmt.Sprintf("%s.png", guest.Token)
		qrFilePath := fmt.Sprintf("./public/qrcodes/%s", qrFileName)
		_ = os.MkdirAll("./public/qrcodes", os.ModePerm)
		if err := qrcode.WriteFile(verifyURL, qrcode.Medium, 256, qrFilePath); err == nil {
			guest.QRImageURL = "/public/qrcodes/" + qrFileName
		}
	}

	guest.Status = newStatus
	if err := DB.Save(&guest).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "فشل الحفظ"})
		return
	}
	
	
// ابعت الباركود فقط لو الحالة بقت confirmed وفيه QR
    if newStatus == "confirmed" && guest.QRImageURL != "" {
        sendQRToGuest(&guest)
        
    }

	c.JSON(http.StatusOK, gin.H{
		"message":        "تم التعديل بنجاح",
		"status_changed": oldStatus != newStatus,
		"guest":          guest,
	})
}

func ensureLocationQR(mapsURL string) string {
	mapsURL = strings.TrimSpace(mapsURL)
	if mapsURL == "" {
		return ""
	}
	_ = os.MkdirAll("./public/qrcodes", 0o755)
	path := "./public/qrcodes/location_qr.png"
	if err := qrcode.WriteFile(mapsURL, qrcode.Medium, 256, path); err != nil {
		fmt.Printf("⚠️ فشل توليد QR الموقع: %v\n", err)
		return ""
	}
	return "/public/qrcodes/location_qr.png"
}

func RenderVerifyPage(c *gin.Context) {
	token := c.Param("token")
	var guest Guest

	if err := DB.Where("token = ?", token).First(&guest).Error; err != nil {
		// أضفنا باقي المتغيرات بقيم فارغة لتجنب خطأ محرك القوالب
		c.HTML(http.StatusOK, "verify.html", gin.H{
			"Error":      "هذا الباركود غير مسجل في النظام!",
			"Name":       "",
			"Companions": 0,
			"Status":     "",
		})
		return
	}

	// أضفنا المتغير Error بقيمة فارغة هنا ليتعرف عليه ملف HTML
	c.HTML(http.StatusOK, "verify.html", gin.H{
		"Error":      "", 
		"Name":       guest.Name,
		"Companions": guest.Companions,
		"Status":     guest.Status,
	})
}

func kuwaitNow() time.Time {
	loc, err := time.LoadLocation("Asia/Kuwait")
	if err != nil {
		return time.Now()
	}
	return time.Now().In(loc)
}

func formatKuwait(t time.Time) string {
	loc, err := time.LoadLocation("Asia/Kuwait")
	if err != nil {
		return t.Format("2006-01-02 15:04")
	}
	return t.In(loc).Format("2006-01-02 15:04")
}

func APIVerify(c *gin.Context) {
	token := c.Param("token")
	var guest Guest

	if err := DB.Where("token = ?", token).First(&guest).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success":            false,
			"error":              "هذا الباركود غير مسجل في النظام!",
			"already_checked_in": false,
		})
		return
	}

	// مش مؤكد أو مفيش باركود صالح → ارفض
	if guest.Status != "confirmed" || guest.QRImageURL == "" {
		msg := "هذا الباركود غير صالح للدخول"
		if guest.Status == "declined" {
			msg = "تم الاعتذار عن الحضور — الباركود ملغي"
		} else if guest.Status == "pending" {
			msg = "الحضور غير مؤكد بعد"
		}
		c.JSON(http.StatusOK, gin.H{
			"success":            false,
			"error":              msg,
			"name":               guest.Name,
			"phone":              guest.Phone,
			"companions":         guest.Companions,
			"status":             guest.Status,
			"checked_in":         guest.CheckedIn,
			"already_checked_in": false,
		})
		return
	}

	// ===== سكان تاني أو أكتر =====
	if guest.CheckedIn {
		checkedAt := ""
		if guest.CheckedInAt != nil {
			checkedAt = formatKuwait(*guest.CheckedInAt)
		}
		c.JSON(http.StatusOK, gin.H{
			"success":            true,
			"name":               guest.Name,
			"phone":              guest.Phone,
			"companions":         guest.Companions,
			"status":             guest.Status,
			"checked_in":         true,
			"already_checked_in": true,
			"checked_in_at":      checkedAt,
			"message":            "تم فحص هذا الباركود مسبقاً - هذا المدعو قام بالدخول",
		})
		return
	}

	// ===== أول سكان ناجح =====
	now := kuwaitNow()
	guest.CheckedIn = true
	guest.CheckedInAt = &now
	if err := DB.Save(&guest).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success":            false,
			"error":              "فشل تسجيل الدخول، حاول مرة أخرى",
			"already_checked_in": false,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":            true,
		"name":               guest.Name,
		"phone":              guest.Phone,
		"companions":         guest.Companions,
		"status":             guest.Status,
		"checked_in":         true,
		"already_checked_in": false,
		"checked_in_at": formatKuwait(now),
		"message":            "تم تسجيل الدخول بنجاح ✅",
	})
}

func getSettings() InvitationSettings {
	var settings InvitationSettings
	DB.First(&settings)
	return settings
}

func RenderSettingsPage(c *gin.Context) {
	settings := getSettings()
	c.HTML(http.StatusOK, "settings.html", gin.H{
		"Settings": settings,
	})
}

func UpdateSettings(c *gin.Context) {
	var settings InvitationSettings
	DB.First(&settings)

	settings.EventTitle = c.PostForm("event_title")
	settings.EventSubtitle = c.PostForm("event_subtitle")
	settings.Person1 = c.PostForm("person1")
	settings.Person2 = c.PostForm("person2")
	settings.DateText = c.PostForm("date_text")
	settings.LocationName = c.PostForm("location_name")
	settings.LocationAddress = c.PostForm("location_address")
	settings.MapsURL = c.PostForm("maps_url")
	_ = ensureLocationQR(settings.MapsURL)
	settings.FooterQuote = c.PostForm("footer_quote")
	settings.PrimaryColor = c.PostForm("primary_color")
	settings.SecondaryColor = c.PostForm("secondary_color")
	settings.EventDate = strings.TrimSpace(c.PostForm("event_date"))
    settings.EventTime = strings.TrimSpace(c.PostForm("event_time"))
    
	// رفع الصور
// رفع الصور + حذف القديمة
os.MkdirAll("./public/uploads", os.ModePerm)

// Logo
file, err := c.FormFile("logo")
if err == nil {
	// حذف الصورة القديمة لو موجودة
	if settings.LogoURL != "" {
		os.Remove("." + settings.LogoURL)
	}
	filename := "logo_" + uuid.New().String() + filepath.Ext(file.Filename)
	path := "./public/uploads/" + filename
	c.SaveUploadedFile(file, path)
	settings.LogoURL = "/public/uploads/" + filename
}

// Background
file, err = c.FormFile("background")
if err == nil {
	if settings.BackgroundURL != "" {
		os.Remove("." + settings.BackgroundURL)
	}
	filename := "bg_" + uuid.New().String() + filepath.Ext(file.Filename)
	path := "./public/uploads/" + filename
	c.SaveUploadedFile(file, path)
	settings.BackgroundURL = "/public/uploads/" + filename
}

// Icon Location
file, err = c.FormFile("icon_location")
if err == nil {
	if settings.IconLocationURL != "" {
		os.Remove("." + settings.IconLocationURL)
	}
	filename := "icon_loc_" + uuid.New().String() + filepath.Ext(file.Filename)
	path := "./public/uploads/" + filename
	c.SaveUploadedFile(file, path)
	settings.IconLocationURL = "/public/uploads/" + filename
}

// Icon Date
file, err = c.FormFile("icon_date")
if err == nil {
	if settings.IconDateURL != "" {
		os.Remove("." + settings.IconDateURL)
	}
	filename := "icon_date_" + uuid.New().String() + filepath.Ext(file.Filename)
	path := "./public/uploads/" + filename
	c.SaveUploadedFile(file, path)
	settings.IconDateURL = "/public/uploads/" + filename
}

// حذف الصور عند اختيار الـ checkbox
if c.PostForm("remove_logo") == "1" {
	if settings.LogoURL != "" {
		os.Remove("." + settings.LogoURL)
	}
	settings.LogoURL = ""
}
if c.PostForm("remove_background") == "1" {
	if settings.BackgroundURL != "" {
		os.Remove("." + settings.BackgroundURL)
	}
	settings.BackgroundURL = ""
}
if c.PostForm("remove_icon_location") == "1" {
	if settings.IconLocationURL != "" {
		os.Remove("." + settings.IconLocationURL)
	}
	settings.IconLocationURL = ""
}
if c.PostForm("remove_icon_date") == "1" {
	if settings.IconDateURL != "" {
		os.Remove("." + settings.IconDateURL)
	}
	settings.IconDateURL = ""
}
	DB.Save(&settings)
	c.Redirect(http.StatusFound, "/admin/settings?success=1")
}
func DeleteGuestsBulk(c *gin.Context) {
	var input struct {
		IDs []uint `json:"ids"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "بيانات غير صالحة"})
		return
	}

	if len(input.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "لم يتم تحديد أي مدعو"})
		return
	}

	// حذف صور الـ QR لو موجودة
	var guests []Guest
	DB.Unscoped().Where("id IN ?", input.IDs).Find(&guests)
	for _, g := range guests {
		if g.QRImageURL != "" {
			os.Remove("." + g.QRImageURL)
		}
	}

	// حذف نهائي
	if err := DB.Unscoped().Where("id IN ?", input.IDs).Delete(&Guest{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "فشل الحذف"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "تم حذف المدعوين المحددين بنجاح"})
}

func ImportGuestsExcel(c *gin.Context) {
	file, err := c.FormFile("excel")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "يجب رفع ملف Excel"})
		return
	}

	tempPath := "./temp_import.xlsx"
	if err := c.SaveUploadedFile(file, tempPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "فشل حفظ الملف"})
		return
	}
	defer os.Remove(tempPath)

	f, err := excelize.OpenFile(tempPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "الملف غير صالح أو تالف"})
		return
	}
	defer f.Close()

	// نجيب أول شيت موجود تلقائياً
	sheetName := f.GetSheetName(0)
	if sheetName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "لا يوجد شيت في الملف"})
		return
	}

	rows, err := f.GetRows(sheetName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "فشل قراءة البيانات من الملف"})
		return
	}

	if len(rows) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "الملف فاضي أو يحتوي على عناوين فقط"})
		return
	}

	successCount := 0
	failCount := 0
	var errorsList []string

	for i, row := range rows {
		if i == 0 {
			continue // تخطي صف العناوين
		}

		if len(row) < 2 {
			failCount++
			continue
		}

		name := strings.TrimSpace(row[0])
		phone := strings.TrimSpace(row[1])
		companions := 0

		if len(row) >= 3 {
			companions, _ = strconv.Atoi(strings.TrimSpace(row[2]))
		}

		if name == "" || phone == "" {
			failCount++
			errorsList = append(errorsList, "صف "+strconv.Itoa(i+1)+" ناقص بيانات")
			continue
		}
		
		gender := "male"
		if len(row) >= 4 {
			g := strings.ToLower(strings.TrimSpace(row[3]))
			if g == "female" || g == "f" || g == "انثى" || g == "أنثى" || g == "نثى" {
				gender = "female"
			}
		}

		guestToken := uuid.New().String()
		newGuest := Guest{
			Name:       name,
			Phone:      phone,
			Companions: companions,
			Gender:     gender,
			Token:      guestToken,
			Status:     "pending",
		}

		if err := DB.Create(&newGuest).Error; err != nil {
			failCount++
			errorsList = append(errorsList, phone+" مسجل مسبقاً")
			continue
		}
		successCount++
	}

	c.JSON(http.StatusOK, gin.H{
		"message":       "تم الاستيراد",
		"success_count": successCount,
		"fail_count":    failCount,
		"errors":        errorsList,
	})
}

func ExportGuestsExcel(c *gin.Context) {
	statusFilter := c.Query("status")   // all | confirmed | declined | pending
	genderFilter := c.Query("gender")   // all | male | female
	checkedFilter := c.Query("checked") // all | yes | no

	var guests []Guest
	query := DB.Model(&Guest{})

	if statusFilter != "" && statusFilter != "all" {
		query = query.Where("status = ?", statusFilter)
	}
	if genderFilter != "" && genderFilter != "all" {
		query = query.Where("gender = ?", genderFilter)
	}
	if checkedFilter == "yes" {
		query = query.Where("checked_in = ?", true)
	} else if checkedFilter == "no" {
		query = query.Where("checked_in = ?", false)
	}

	query.Order("id asc").Find(&guests)

	f := excelize.NewFile()
	sheet := "الضيوف"
	f.SetSheetName("Sheet1", sheet)

	// العناوين
	headers := []string{"م", "الاسم", "الهاتف", "الجنس", "المرافقين", "الحالة", "الدخول", "وقت الدخول", "تاريخ التسجيل"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, h)
	}

	// تنسيق العناوين
	style, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"1B4332"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	})
	f.SetCellStyle(sheet, "A1", "I1", style)

	for i, g := range guests {
		row := i + 2
		gender := "ذكر"
		if g.Gender == "female" {
			gender = "أنثى"
		}
		status := "قيد الانتظار"
		switch g.Status {
		case "confirmed":
			status = "مؤكد"
		case "declined":
			status = "معتذر"
		}
		checked := "لم يدخل"
		checkedAt := "—"
		if g.CheckedIn {
			checked = "تم الدخول"
			if g.CheckedInAt != nil {
				checkedAt = formatKuwait(*g.CheckedInAt)
			}
		}

		f.SetCellValue(sheet, fmt.Sprintf("A%d", row), g.ID)
		f.SetCellValue(sheet, fmt.Sprintf("B%d", row), g.Name)
		f.SetCellValue(sheet, fmt.Sprintf("C%d", row), g.Phone)
		f.SetCellValue(sheet, fmt.Sprintf("D%d", row), gender)
		f.SetCellValue(sheet, fmt.Sprintf("E%d", row), g.Companions)
		f.SetCellValue(sheet, fmt.Sprintf("F%d", row), status)
		f.SetCellValue(sheet, fmt.Sprintf("G%d", row), checked)
		f.SetCellValue(sheet, fmt.Sprintf("H%d", row), checkedAt)
		f.SetCellValue(sheet, fmt.Sprintf("I%d", row), formatKuwait(g.CreatedAt))
	}

	// عرض الأعمدة
	f.SetColWidth(sheet, "A", "A", 8)
	f.SetColWidth(sheet, "B", "B", 22)
	f.SetColWidth(sheet, "C", "C", 16)
	f.SetColWidth(sheet, "D", "I", 14)

	filename := fmt.Sprintf("guests_export_%s.xlsx", time.Now().Format("2006-01-02_15-04"))
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Header("Content-Transfer-Encoding", "binary")
	_ = f.Write(c.Writer)
}

// آخر موعد للرد = يوم الزفاف ناقص 3 أيام (نهاية اليوم بتوقيت القاهرة)
func rsvpDeadline() (time.Time, bool) {
	s := getSettings()
	if strings.TrimSpace(s.EventDate) == "" {
		return time.Time{}, false // مفيش تاريخ → مفيش إغلاق تلقائي
	}
    loc, err := time.LoadLocation("Asia/Kuwait")
	if err != nil {
		loc = time.Local
	}
	eventDay, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(s.EventDate), loc)
	if err != nil {
		return time.Time{}, false
	}
	// قبل الزفاف بـ 3 أيام، الساعة 23:59:59
	deadline := eventDay.AddDate(0, 0, -3).Add(23*time.Hour + 59*time.Minute + 59*time.Second)
	return deadline, true
}

func isRSVPClosed() bool {
	deadline, ok := rsvpDeadline()
	if !ok {
		return false
	}
	return kuwaitNow().After(deadline)
}

func rsvpClosedMessage() string {
	deadline, ok := rsvpDeadline()
	if !ok {
		return "انتهت فترة تأكيد الحضور أو الاعتذار.\nللاستفسار تواصل مع الإدارة."
	}
	return fmt.Sprintf(
		"انتهت فترة تأكيد الحضور أو الاعتذار.\nكان آخر موعد للرد: %s\nللاستفسار تواصل مع الإدارة.",
		formatKuwait(deadline),
	)
}
func getAdminRole(c *gin.Context) string {
	user, exists := c.Get(gin.AuthUserKey)
	if !exists {
		return ""
	}
	username := user.(string)

	var admin AdminUser
	if err := DB.Where("username = ?", username).First(&admin).Error; err != nil {
		return ""
	}
	return admin.Role
}

func requireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role := getAdminRole(c)
		for _, r := range roles {
			if role == r {
				c.Next()
				return
			}
		}
		c.String(http.StatusForbidden, "ليس لديك صلاحية للوصول لهذه الصفحة")
		c.Abort()
	}
}

func RenderUsersPage(c *gin.Context) {
	var users []AdminUser
	DB.Find(&users)
	c.HTML(http.StatusOK, "users.html", gin.H{
		"Users": users,
		"Role":  getAdminRole(c),
	})
}

func CreateAdminUser(c *gin.Context) {
	username := strings.TrimSpace(c.PostForm("username"))
	password := strings.TrimSpace(c.PostForm("password"))
	role := strings.TrimSpace(c.PostForm("role"))
	name := strings.TrimSpace(c.PostForm("name"))

	if username == "" || password == "" || role == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "كل الحقول مطلوبة"})
		return
	}
	if role != "manager" && role != "scanner" && role != "reception" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "صلاحية غير صحيحة"})
		return
	}

	user := AdminUser{Username: username, Password: password, Role: role, Name: name}
	if err := DB.Create(&user).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "اسم المستخدم موجود مسبقاً"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "تم إضافة المستخدم"})
}

func DeleteAdminUser(c *gin.Context) {
	id := c.Param("id")
	var user AdminUser
	if err := DB.First(&user, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "المستخدم غير موجود"})
		return
	}
	// منع حذف آخر مدير
	if user.Role == "manager" {
		var count int64
		DB.Model(&AdminUser{}).Where("role = ?", "manager").Count(&count)
		if count <= 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "لا يمكن حذف آخر مدير"})
			return
		}
	}
	DB.Delete(&user)
	c.JSON(http.StatusOK, gin.H{"message": "تم الحذف"})
}

// Middleware تحقق من المستخدم من قاعدة البيانات في كل طلب
func dynamicBasicAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		user, pass, ok := c.Request.BasicAuth()
		if !ok {
			c.Header("WWW-Authenticate", `Basic realm="Admin"`)
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		var admin AdminUser
		if err := DB.Where("username = ?", user).First(&admin).Error; err != nil {
			c.Header("WWW-Authenticate", `Basic realm="Admin"`)
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		if admin.Password != pass {
			c.Header("WWW-Authenticate", `Basic realm="Admin"`)
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// نخزن اسم المستخدم عشان getAdminRole يشتغل
		c.Set(gin.AuthUserKey, admin.Username)
		c.Next()
	}
}

func main() {
	if err := godotenv.Load(); err != nil {
		fmt.Println("⚠️ لم يتم تحميل .env:", err)
	} else {
		fmt.Println("✅ تم تحميل .env")
	}
	// اطبع للتأكد (بدون طباعة التوكن كامل)
	t := os.Getenv("WA_CLOUD_TOKEN")
	fmt.Printf("TOKEN len=%d | PHONE_ID=%s\n", len(t), os.Getenv("WA_PHONE_NUMBER_ID"))
	
	ConnectDB()
	ConnectGradDB()
	
    InitWhatsApp()
    StartWeddingReminder()
	r := gin.Default()

	// دالة مساعدة لتنسيق التاريخ داخل الـ HTML
r.SetFuncMap(template.FuncMap{
"formatDate": func(v interface{}) string {
		loc, _ := time.LoadLocation("Asia/Kuwait")
		switch t := v.(type) {
		case time.Time:
			if t.IsZero() {
				return "—"
			}
			if loc != nil {
				return t.In(loc).Format("2006-01-02 15:04")
			}
			return t.Format("2006-01-02 15:04")
		case *time.Time:
			if t == nil || t.IsZero() {
				return "—"
			}
			if loc != nil {
				return t.In(loc).Format("2006-01-02 15:04")
			}
			return t.Format("2006-01-02 15:04")
		default:
			return "—"
		
		}
	},
})

	r.LoadHTMLGlob("templates/*")
	r.LoadHTMLGlob("templates/grad/*")
	r.Static("/public", "./public")

	// =====================================
	// المسارات العامة (بدون حماية)
	// =====================================
	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/login")
	})
	r.GET("/login", func(c *gin.Context) {
	settings := getSettings()
	c.HTML(http.StatusOK, "login.html", gin.H{
		"Settings": settings,
	})
})
	r.GET("/invite/:token", RenderInvitePage)
	r.GET("/ticket/:token", RenderTicketPage)
// Webhook (عام — Meta بتنده)
    r.GET("/webhook/whatsapp", CloudWebhookVerifyHandler)
    r.POST("/webhook/whatsapp", CloudWebhookReceiveHandler)
	
	api := r.Group("/api")
	{
		api.POST("/guests", CreateGuest)
		api.POST("/login", LoginByPhone)
		api.POST("/rsvp", UpdateRSVP)
	}

	// =====================================
	// المسارات المحمية (بكلمة مرور للأدمن)
	// =====================================
    // بناء قائمة الحسابات من قاعدة البيانات
    adminAuth := r.Group("/", dynamicBasicAuth())
{
	// الجميع يقدروا يدخلوا الداشبورد (بس المحتوى يختلف)
	adminAuth.GET("/admin/dashboard", RenderDashboard)

	// المدير فقط
	managerOnly := adminAuth.Group("/", requireRole("manager"))
	{
		managerOnly.GET("/admin/add", func(c *gin.Context) {
			c.HTML(http.StatusOK, "add_guest.html", gin.H{})
		})
		managerOnly.GET("/admin/settings", RenderSettingsPage)
		managerOnly.POST("/admin/settings", UpdateSettings)
		managerOnly.POST("/admin/api/import-excel", ImportGuestsExcel)
		managerOnly.POST("/admin/api/guests/bulk-delete", DeleteGuestsBulk)
		managerOnly.DELETE("/admin/api/guests/:id", DeleteGuestAdmin)
		managerOnly.PUT("/admin/api/guests/:id", UpdateGuestAdmin)
		managerOnly.POST("/admin/api/broadcast-whatsapp", BroadcastWhatsAppHandler)
		managerOnly.POST("/admin/api/broadcast-cloud", BroadcastCloudHandler)
		managerOnly.POST("/admin/api/cloud-test-send", CloudTestSendHandler)
		managerOnly.GET("/admin/api/whatsapp-status", WhatsAppStatusHandler)
		managerOnly.POST("/admin/api/whatsapp-logout", LogoutWhatsAppHandler)
		managerOnly.GET("/admin/api/export-excel", ExportGuestsExcel)
		managerOnly.GET("/admin/users", RenderUsersPage)
        managerOnly.POST("/admin/api/users", CreateAdminUser)
        managerOnly.DELETE("/admin/api/users/:id", DeleteAdminUser)
	}

	// المسح + المدير
	scanAccess := adminAuth.Group("/", requireRole("manager", "scanner"))
	{
		scanAccess.GET("/scan", func(c *gin.Context) {
			c.HTML(http.StatusOK, "scanner.html", gin.H{})
		})
		scanAccess.GET("/api/verify/:token", APIVerify)
		scanAccess.GET("/verify/:token", RenderVerifyPage)
	}

	// الاستقبال + المدير (يشوف القوائم بدون تعديل)
	receptionAccess := adminAuth.Group("/", requireRole("manager", "reception"))
	{
		receptionAccess.GET("/admin/print/:status", PrintReport)
	}
}

// =====================================
// نظام حفل التخرج (منفصل)
// =====================================
gradAuth := r.Group("/grad", dynamicBasicAuth())
{
	gradAuth.GET("/admin/dashboard", GradRenderDashboard)

	gradManager := gradAuth.Group("/", requireRole("manager"))
	{
		gradManager.GET("/admin/add", func(c *gin.Context) {
			c.HTML(http.StatusOK, "grad_add_guest.html", gin.H{})
		})
		gradManager.GET("/admin/settings", GradRenderSettings)
		gradManager.POST("/admin/settings", GradUpdateSettings)
		gradManager.POST("/admin/api/guests", GradCreateGuest)
		gradManager.DELETE("/admin/api/guests/:id", GradDeleteGuest)
		gradManager.PUT("/admin/api/guests/:id", GradUpdateGuest)
		gradManager.POST("/admin/api/import-excel", GradImportExcel)
		gradManager.POST("/admin/api/broadcast", GradBroadcastHandler) // whatsmeow فقط
		gradManager.GET("/admin/api/export-excel", GradExportExcel)
	}

	gradScan := gradAuth.Group("/", requireRole("manager", "scanner"))
	{
		gradScan.GET("/scan", func(c *gin.Context) {
			c.HTML(http.StatusOK, "grad_scanner.html", gin.H{})
		})
		gradScan.GET("/api/verify/:token", GradAPIVerify)
		gradScan.GET("/verify/:token", GradRenderVerifyPage)
	}
}

// صفحة عامة للمدعو (عرض الدعوة + الباركود) — بدون تأكيد/اعتذار
r.GET("/grad/invite/:token", GradRenderInvitePage)
// في main.go — مسارات عامة
r.GET("/grad/public-verify/:token", func(c *gin.Context) {
	if c.Query("key") != os.Getenv("GRAD_SCAN_KEY") {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "غير مصرح"})
		return
	}
	GradAPIVerify(c)
})
	port := os.Getenv("PORT")
	if port == "" {
    	port = "8080"
	}
	
	// تشغيل الخادم على جميع واجهات الشبكة
	r.Run("0.0.0.0:" + port)

}
