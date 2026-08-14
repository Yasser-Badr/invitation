package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"time"
	"path/filepath"

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
	Name       string
	Phone      string `gorm:"uniqueIndex"`
	Companions int    `gorm:"default:0"`
	Token      string `gorm:"uniqueIndex"`
	QRImageURL string
	Status     string `gorm:"default:'pending'"`
	CheckedIn  bool   `gorm:"default:false"`
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
}

var DB *gorm.DB

func ConnectDB() {
	database, err := gorm.Open(sqlite.Open("wedding_test.db"), &gorm.Config{})
	if err != nil {
		log.Fatal("❌ فشل الاتصال بقاعدة البيانات:", err)
	}

	err = database.AutoMigrate(&Guest{}, &InvitationSettings{})
	if err != nil {
		log.Fatal("❌ فشل عمل Migration:", err)
	}
	DB = database

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
}

func CreateGuest(c *gin.Context) {
	var input CreateGuestInput

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	guestToken := uuid.New().String()

	newGuest := Guest{
		Name:       input.Name,
		Phone:      input.Phone,
		Companions: input.Companions,
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

	settings := getSettings()

	c.HTML(http.StatusOK, "invite.html", gin.H{
		"Name":       guest.Name,
		"Companions": guest.Companions,
		"Token":      guest.Token,
		"Settings":   settings,
	})
}

type RSVPInput struct {
	Token  string `json:"token" binding:"required"`
	Status string `json:"status" binding:"required"`
}

func UpdateRSVP(c *gin.Context) {
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

	// جلب جميع البيانات وتقسيمها
	DB.Find(&allGuests)
	DB.Where("status = ?", "confirmed").Find(&confirmedGuests)
	DB.Where("status = ?", "declined").Find(&declinedGuests)

	c.HTML(http.StatusOK, "dashboard.html", gin.H{
		"AllGuests": allGuests,
		"Confirmed": confirmedGuests,
		"Declined":  declinedGuests,
	})
}

// === دالة الطباعة المنفصلة ===
func PrintReport(c *gin.Context) {
	status := c.Param("status")
	var guests []Guest
	var title string

	if status == "confirmed" {
		DB.Where("status = ?", "confirmed").Find(&guests)
		title = "قائمة مؤكدي الحضور"
	} else if status == "declined" {
		DB.Where("status = ?", "declined").Find(&guests)
		title = "قائمة المعتذرين عن الحضور"
	} else if status == "all" {
		DB.Find(&guests) // جلب جميع المدعوين
		title = "قائمة جميع المدعوين"
	} else {
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

	guest.Name = input.Name
	guest.Phone = input.Phone
	guest.Companions = input.Companions
	guest.CheckedIn = input.CheckedIn
	DB.Save(&guest)
	c.JSON(http.StatusOK, gin.H{"message": "تم التعديل بنجاح"})
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
func APIVerify(c *gin.Context) {
	token := c.Param("token")
	var guest Guest

	if err := DB.Where("token = ?", token).First(&guest).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   "هذا الباركود غير مسجل في النظام!",
		})
		return
	}

	// لو الضيف مش مؤكد حضوره
	if guest.Status != "confirmed" {
		c.JSON(http.StatusOK, gin.H{
			"success":    true,
			"name":       guest.Name,
			"phone":      guest.Phone,
			"companions": guest.Companions,
			"status":     guest.Status,
			"checked_in": false,
		})
		return
	}

	// لو تم فحصه من قبل
	if guest.CheckedIn {
		c.JSON(http.StatusOK, gin.H{
			"success":    true,
			"name":       guest.Name,
			"phone":      guest.Phone,
			"companions": guest.Companions,
			"status":     guest.Status,
			"checked_in": true, // ← علامة إنه دخل قبل كده
			"message":    "تم فحص هذا الباركود مسبقاً - هذا المدعو قام بالدخول",
		})
		return
	}

	// أول مرة يتم فحصه → نسجله كـ دخل
	guest.CheckedIn = true
	DB.Save(&guest)

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"name":       guest.Name,
		"phone":      guest.Phone,
		"companions": guest.Companions,
		"status":     guest.Status,
		"checked_in": false, // أول مرة
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
	settings.FooterQuote = c.PostForm("footer_quote")
	settings.PrimaryColor = c.PostForm("primary_color")
	settings.SecondaryColor = c.PostForm("secondary_color")

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

		guestToken := uuid.New().String()
		newGuest := Guest{
			Name:       name,
			Phone:      phone,
			Companions: companions,
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

func main() {
	ConnectDB()

	r := gin.Default()

	// دالة مساعدة لتنسيق التاريخ داخل الـ HTML
	r.SetFuncMap(template.FuncMap{
		"formatDate": func(t time.Time) string {
			return t.Format("2006-01-02 15:04")
		},
	})

	r.LoadHTMLGlob("templates/*")
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

	api := r.Group("/api")
	{
		api.POST("/guests", CreateGuest)
		api.POST("/login", LoginByPhone)
		api.POST("/rsvp", UpdateRSVP)
	}

	// =====================================
	// المسارات المحمية (بكلمة مرور للأدمن)
	// =====================================
	adminAuth := r.Group("/", gin.BasicAuth(gin.Accounts{
		"Yaaser Badr": "Yasser.12#", // اسم المستخدم والباسورد كما طلبت
	}))
	{
		adminAuth.GET("/admin/dashboard", RenderDashboard)
		adminAuth.GET("/admin/add", func(c *gin.Context) {
			c.HTML(http.StatusOK, "add_guest.html", gin.H{})
		})
		//adminAuth.GET("/verify/:token", RenderVerifyPage)
		
		// مسارات التحديثات الجديدة
		adminAuth.GET("/admin/print/:status", PrintReport)
		adminAuth.DELETE("/admin/api/guests/:id", DeleteGuestAdmin)
		adminAuth.PUT("/admin/api/guests/:id", UpdateGuestAdmin)

		adminAuth.GET("/admin/settings", RenderSettingsPage)
        adminAuth.POST("/admin/settings", UpdateSettings)
	
        adminAuth.POST("/admin/api/import-excel", ImportGuestsExcel)
        adminAuth.POST("/admin/api/guests/bulk-delete", DeleteGuestsBulk)

	    adminAuth.GET("/scan", func(c *gin.Context) {
		    c.HTML(http.StatusOK, "scanner.html", gin.H{})
			adminAuth.GET("/api/verify/:token", APIVerify)
		})
	    adminAuth.GET("/verify/:token", RenderVerifyPage)


	}
	
	port := os.Getenv("PORT")
	if port == "" {
		port = "9000"
	}
	
	// تشغيل الخادم على جميع واجهات الشبكة
	r.Run("0.0.0.0:" + port)
}
