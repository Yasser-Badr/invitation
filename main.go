package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/skip2/go-qrcode"
	"gorm.io/driver/sqlite" // تم التغيير هنا لاستخدام SQLite
	"gorm.io/gorm"
)

// ==========================================
// 1. Database Model
// ==========================================
type Guest struct {
	gorm.Model
	Name       string
	Phone      string
	Token      string `gorm:"uniqueIndex"`
	QRImageURL string
	Status     string `gorm:"default:'pending'"`
}

var DB *gorm.DB

// ==========================================
// 2. Database Connection (SQLite)
// ==========================================
func ConnectDB() {
	// استخدام SQLite وإنشاء ملف باسم wedding_test.db
	database, err := gorm.Open(sqlite.Open("wedding_test.db"), &gorm.Config{})
	if err != nil {
		log.Fatal("❌ فشل الاتصال بقاعدة البيانات:", err)
	}

	err = database.AutoMigrate(&Guest{})
	if err != nil {
		log.Fatal("❌ فشل عمل Migration:", err)
	}

	log.Println("✅ تم إنشاء/الاتصال بملف SQLite وعمل Migration بنجاح!")
	DB = database
}

// ==========================================
// 3. Controllers 
// ==========================================
type CreateGuestInput struct {
	Name  string `json:"name" binding:"required"`
	Phone string `json:"phone" binding:"required"`
}

func CreateGuest(c *gin.Context) {
	var input CreateGuestInput

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	guestToken := uuid.New().String()
	inviteURL := fmt.Sprintf("http://localhost:8080/invite/%s", guestToken)

	qrFileName := fmt.Sprintf("%s.png", guestToken)
	qrFilePath := fmt.Sprintf("./public/qrcodes/%s", qrFileName)
	// --- هذا هو الكود الجديد لحل المشكلة ---
	// سيقوم بإنشاء مجلد public وبداخله qrcodes إذا لم يكونوا موجودين
	os.MkdirAll("./public/qrcodes", os.ModePerm)
	// ----------------------------------------

	err := qrcode.WriteFile(inviteURL, qrcode.Medium, 256, qrFilePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "فشل في توليد الباركود"})
		return
	}

	newGuest := Guest{
		Name:       input.Name,
		Phone:      input.Phone,
		Token:      guestToken,
		QRImageURL: fmt.Sprintf("/public/qrcodes/%s", qrFileName),
		Status:     "pending",
	}

	DB.Create(&newGuest)

	c.JSON(http.StatusCreated, gin.H{
		"message": "تم إضافة المدعو وإنشاء الباركود بنجاح",
		"data":    newGuest,
	})
}

type RSVPInput struct {
	Token  string `json:"token" binding:"required"`
	Status string `json:"status" binding:"required"`
}

func RenderInvitePage(c *gin.Context) {
	token := c.Param("token")
	var guest Guest

	if err := DB.Where("token = ?", token).First(&guest).Error; err != nil {
		c.String(http.StatusNotFound, "الدعوة غير صالحة أو غير موجودة")
		return
	}

	c.HTML(http.StatusOK, "invite.html", gin.H{
		"Name":  guest.Name,
		"Token": guest.Token,
	})
}

func UpdateRSVP(c *gin.Context) {
	var input RSVPInput

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "بيانات غير صالحة"})
		return
	}

	result := DB.Model(&Guest{}).Where("token = ?", input.Token).Update("status", input.Status)
	
	if result.Error != nil || result.RowsAffected == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "فشل في تحديث الحالة"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "تم تحديث حالة الحضور بنجاح"})
}

// ==========================================
// 4. Main Function
// ==========================================
func main() {
	ConnectDB()

	r := gin.Default()

	r.LoadHTMLGlob("templates/*")
	r.Static("/public", "./public")

		// مسار عرض صفحة الدعوة للضيوف
	r.GET("/invite/:token", RenderInvitePage)

	// --- الكود الجديد ---
	// مسار لوحة التحكم لإضافة الضيوف
	r.GET("/admin/add", func(c *gin.Context) {
		c.HTML(http.StatusOK, "add_guest.html", gin.H{})
	})
	// -------------------

	api := r.Group("/api")
	{
		api.POST("/guests", CreateGuest)
		api.POST("/rsvp", UpdateRSVP)
	}

	log.Println("🚀 الخادم يعمل الآن على الرابط: http://localhost:8080")
	r.Run(":8080")
}