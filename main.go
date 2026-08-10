package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/skip2/go-qrcode"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Guest struct {
	gorm.Model
	Name       string
	Phone      string `gorm:"uniqueIndex"`
	Companions int    `gorm:"default:0"`
	Token      string `gorm:"uniqueIndex"`
	QRImageURL string
	Status     string `gorm:"default:'pending'"`
}

var DB *gorm.DB

func ConnectDB() {
	database, err := gorm.Open(sqlite.Open("wedding_test.db"), &gorm.Config{})
	if err != nil {
		log.Fatal("❌ فشل الاتصال بقاعدة البيانات:", err)
	}

	err = database.AutoMigrate(&Guest{})
	if err != nil {
		log.Fatal("❌ فشل عمل Migration:", err)
	}
	DB = database
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

	c.HTML(http.StatusOK, "invite.html", gin.H{
		"Name":       guest.Name,
		"Companions": guest.Companions,
		"Token":      guest.Token,
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

	c.HTML(http.StatusOK, "ticket.html", gin.H{
		"ID":         guest.ID,
		"Name":       guest.Name,
		"Phone":      guest.Phone,
		"Companions": guest.Companions,
		"QRImageURL": guest.QRImageURL,
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
func UpdateGuestAdmin(c *gin.Context) {
	id := c.Param("id")
	var input CreateGuestInput
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
	DB.Save(&guest)
	c.JSON(http.StatusOK, gin.H{"message": "تم التعديل بنجاح"})
}

func RenderVerifyPage(c *gin.Context) {
	token := c.Param("token")
	var guest Guest

	if err := DB.Where("token = ?", token).First(&guest).Error; err != nil {
		c.HTML(http.StatusOK, "verify.html", gin.H{"Error": "هذا الباركود غير مسجل في النظام!"})
		return
	}

	c.HTML(http.StatusOK, "verify.html", gin.H{
		"Name":       guest.Name,
		"Companions": guest.Companions,
		"Status":     guest.Status,
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
		c.HTML(http.StatusOK, "login.html", gin.H{})
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
		adminAuth.GET("/verify/:token", RenderVerifyPage)
		
		// مسارات التحديثات الجديدة
		adminAuth.GET("/admin/print/:status", PrintReport)
		adminAuth.DELETE("/admin/api/guests/:id", DeleteGuestAdmin)
		adminAuth.PUT("/admin/api/guests/:id", UpdateGuestAdmin)
	}
	
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	
	// تشغيل الخادم على جميع واجهات الشبكة
	r.Run("0.0.0.0:" + port)
}
