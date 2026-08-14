package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

var WAClient *whatsmeow.Client
var CurrentQRBase64 string
var qrMutex sync.Mutex
var isConnecting bool

// تهيئة اتصال الواتساب
func InitWhatsApp() {
	dbLog := waLog.Stdout("Database", "WARN", true)
	container, err := sqlstore.New(context.Background(), "sqlite3", "file:wa_store.db?_foreign_keys=on", dbLog)
	if err != nil {
		fmt.Printf("❌ فشل إنشاء قاعدة بيانات الجلسة: %v\n", err)
		return
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		fmt.Printf("❌ فشل الحصول على بيانات الجهاز: %v\n", err)
		return
	}

	clientLog := waLog.Stdout("Client", "INFO", true)
	WAClient = whatsmeow.NewClient(deviceStore, clientLog)

	// لو الجلسة موجودة مسبقاً → اتصل تلقائياً
	if WAClient.Store.ID != nil {
		err = WAClient.Connect()
		if err == nil {
			fmt.Println("✅ تم الاتصال التلقائي بالواتساب!")
		} else {
			fmt.Printf("❌ فشل الاتصال التلقائي: %v\n", err)
		}
	} else {
		fmt.Println("⏳ لم يتم الربط بعد. سيتم توليد QR عند الطلب.")
	}
}

// دالة لتوليد QR جديد عند الطلب
func StartQRLogin() {
	qrMutex.Lock()
	defer qrMutex.Unlock()

	if WAClient == nil {
		return
	}

	// لو متصل خلاص مفيش داعي
	if WAClient.IsConnected() && WAClient.Store.ID != nil {
		return
	}

	// لو فيه عملية اتصال جارية خلاص
	if isConnecting {
		return
	}

	isConnecting = true
	CurrentQRBase64 = ""

	// لو فيه اتصال قديم افصله
	if WAClient.IsConnected() {
		WAClient.Disconnect()
	}

	qrChan, err := WAClient.GetQRChannel(context.Background())
	if err != nil {
		fmt.Printf("❌ فشل الحصول على قناة الـ QR: %v\n", err)
		isConnecting = false
		return
	}

	err = WAClient.Connect()
	if err != nil {
		fmt.Printf("❌ فشل الاتصال بالواتساب: %v\n", err)
		isConnecting = false
		return
	}

	go func() {
		for evt := range qrChan {
			fmt.Printf("📱 حدث QR: %s\n", evt.Event)

			switch evt.Event {
			case "code":
				png, err := qrcode.Encode(evt.Code, qrcode.Medium, 256)
				if err != nil {
					fmt.Printf("❌ فشل توليد صورة الـ QR: %v\n", err)
					continue
				}
				qrMutex.Lock()
				CurrentQRBase64 = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
				qrMutex.Unlock()
				fmt.Println("✅ تم توليد صورة الـ QR بنجاح")

			case "timeout":
				qrMutex.Lock()
				CurrentQRBase64 = ""
				isConnecting = false
				qrMutex.Unlock()
				fmt.Println("⏰ انتهت مهلة الـ QR")

			case "success":
				qrMutex.Lock()
				CurrentQRBase64 = ""
				isConnecting = false
				qrMutex.Unlock()
				fmt.Println("✅ تم الربط بحساب الواتساب بنجاح!")

			default:
				if evt.Error != nil {
					fmt.Printf("⚠️ خطأ في QR: %v\n", evt.Error)
				}
			}
		}
		qrMutex.Lock()
		isConnecting = false
		qrMutex.Unlock()
	}()
}

// مسار إرجاع حالة الواتساب
func WhatsAppStatusHandler(c *gin.Context) {
	if WAClient != nil && WAClient.IsConnected() && WAClient.Store.ID != nil {
		c.JSON(http.StatusOK, gin.H{"connected": true})
		return
	}

	// لو مش متصل → ابدأ توليد QR لو مفيش واحد شغال
	qrMutex.Lock()
	qr := CurrentQRBase64
	connecting := isConnecting
	qrMutex.Unlock()

	if qr == "" && !connecting {
		// ابدأ توليد QR جديد
		go StartQRLogin()
	}

	c.JSON(http.StatusOK, gin.H{
		"connected": false,
		"qr":        qr,
	})
}

// دالة الإرسال الفردي
func SendWAMessage(phone string, message string) error {
	if WAClient == nil || !WAClient.IsConnected() {
		return fmt.Errorf("حساب الواتساب غير متصل")
	}
	phone = strings.TrimSpace(phone)
	phone = strings.ReplaceAll(phone, "+", "")
	phone = strings.ReplaceAll(phone, " ", "")
	jid := types.NewJID(phone, types.DefaultUserServer)
	req := &waProto.Message{Conversation: proto.String(message)}
	_, err := WAClient.SendMessage(context.Background(), jid, req)
	return err
}

type BroadcastInput struct {
	MessageText string `json:"message_text" binding:"required"`
}

// دالة الإرسال الجماعي
func BroadcastWhatsAppHandler(c *gin.Context) {
	var input BroadcastInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "نص الرسالة مطلوب"})
		return
	}

	if WAClient == nil || !WAClient.IsConnected() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "الواتساب غير متصل"})
		return
	}

	var guests []Guest
	DB.Find(&guests)

	scheme := "http"
	if c.Request.TLS != nil || c.Request.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	baseURL := fmt.Sprintf("%s://%s", scheme, c.Request.Host)

	var wg sync.WaitGroup
	for _, guest := range guests {
		if strings.TrimSpace(guest.Phone) == "" {
			continue
		}
		wg.Add(1)
		go func(g Guest) {
			defer wg.Done()
			loginLink := fmt.Sprintf("%s/login", baseURL)
			personalizedMsg := strings.ReplaceAll(input.MessageText, "{name}", g.Name)
			fullMessage := fmt.Sprintf("%s\n\nرابط تأكيد الحضور:\n%s", personalizedMsg, loginLink)
			SendWAMessage(g.Phone, fullMessage)
		}(guest)
	}
	wg.Wait()
	c.JSON(http.StatusOK, gin.H{"message": "تم إرسال جميع الرسائل بنجاح!"})
}