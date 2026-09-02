package main

import (
	"log"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// قاعدة بيانات منفصلة تمامًا
var GradDB *gorm.DB

type GradGuest struct {
	gorm.Model
	Name         string
	Phone        string `gorm:"uniqueIndex"`
	Companions   int    `gorm:"default:0"`
	Gender       string `gorm:"default:'male'"`
	Token        string `gorm:"uniqueIndex"`
	QRImageURL   string
	InviteSent   bool       `gorm:"default:false"`
	InviteSentAt *time.Time
	CheckedIn    bool       `gorm:"default:false"`
	CheckedInAt  *time.Time
}

type GradSettings struct {
	gorm.Model
	EventTitle      string // مثال: دعوة
	EventSubtitle   string // لخريجي وخريجات جامعة جدة لعام 2026
	MainLine        string // لحضور حفل التخرج
	SubLine         string // وذلك بتسجيل الحضور من خلال الباركود
	DateText        string
	LocationName    string
	LocationAddress string
	MapsURL         string
	FooterNote      string // للاستفسار: ...
	PrimaryColor    string
	SecondaryColor  string
	LogoURL         string // شعار الجامعة
	BackgroundURL   string // خلفية التصميم
	EventDate       string // YYYY-MM-DD
	EventTime       string // 19:00
}

func ConnectGradDB() {
	database, err := gorm.Open(sqlite.Open("graduation.db"), &gorm.Config{})
	if err != nil {
		log.Fatal("❌ فشل الاتصال بقاعدة بيانات التخرج:", err)
	}
	if err := database.AutoMigrate(&GradGuest{}, &GradSettings{}); err != nil {
		log.Fatal("❌ فشل Migration التخرج:", err)
	}
	GradDB = database

	var count int64
	GradDB.Model(&GradSettings{}).Count(&count)
	if count == 0 {
		GradDB.Create(&GradSettings{
			EventTitle:    "دعوة",
			EventSubtitle: "لخريجي وخريجات جامعة جدة لعام 2026",
			MainLine:      "لحضور حفل التخرج",
			SubLine:       "وذلك بتسجيل الحضور من خلال الباركود",
			DateText:      "",
			LocationName:  "",
			FooterNote:    "للاستفسار: طلاب Dar@uj.edu.sa | طالبات darg-feedback@uj.edu.sa",
			PrimaryColor:  "#1a365d",
			SecondaryColor: "#ffffff",
		})
	}
}

func getGradSettings() GradSettings {
	var s GradSettings
	GradDB.First(&s)
	return s
}