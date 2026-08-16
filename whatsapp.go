package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
		go StartQRLogin()
	}

	c.JSON(http.StatusOK, gin.H{
		"connected": false,
		"qr":        qr,
	})
}

// تنظيف وتوحيد صيغة الرقم
func normalizePhone(phone string) string {
	// 1. تنظيف الرقم من الرموز والمسافات
	phone = strings.TrimSpace(phone)
	phone = strings.ReplaceAll(phone, "+", "")
	phone = strings.ReplaceAll(phone, " ", "")
	phone = strings.ReplaceAll(phone, "-", "")
	phone = strings.ReplaceAll(phone, "(", "")
	phone = strings.ReplaceAll(phone, ")", "")
	phone = strings.ReplaceAll(phone, ".", "")

	if phone == "" {
		return phone
	}

	// 2. لو الرقم بيبدأ برمز دولة معروف خلاص نرجعه زي ما هو
	knownCodes := []string{
		"20",  // مصر
		"966", // السعودية
		"971", // الإمارات
		"965", // الكويت
		"973", // البحرين
		"974", // قطر
		"968", // عمان
		"962", // الأردن
		"961", // لبنان
		"963", // سوريا
		"964", // العراق
		"218", // ليبيا
		"212", // المغرب
		"213", // الجزائر
		"216", // تونس
		"249", // السودان
		"970", // فلسطين
		"967", // اليمن
	}

	for _, code := range knownCodes {
		if strings.HasPrefix(phone, code) {
			return phone // الرمز موجود خلاص
		}
	}

	// 3. لو مفيش رمز دولة → نحاول نخمن حسب شكل الرقم

	// مصر: يبدأ بـ 01 وطوله 11 → 201xxxxxxxxx
	if strings.HasPrefix(phone, "01") && len(phone) == 11 {
		return "20" + phone[1:]
	}
	// مصر: يبدأ بـ 1 وطوله 10 (من غير الصفر)
	if strings.HasPrefix(phone, "1") && len(phone) == 10 {
		return "20" + phone
	}

	// السعودية: يبدأ بـ 05 وطوله 10 → 9665xxxxxxxx
	if strings.HasPrefix(phone, "05") && len(phone) == 10 {
		return "966" + phone[1:]
	}
	// السعودية: يبدأ بـ 5 وطوله 9
	if strings.HasPrefix(phone, "5") && len(phone) == 9 {
		return "966" + phone
	}

	// الكويت: يبدأ بـ 5 أو 6 أو 9 وطوله 8
	if len(phone) == 8 && (strings.HasPrefix(phone, "5") || strings.HasPrefix(phone, "6") || strings.HasPrefix(phone, "9")) {
		return "965" + phone
	}

	// قطر: يبدأ بـ 3 أو 5 أو 6 أو 7 وطوله 8
	if len(phone) == 8 && (phone[0] >= '3' && phone[0] <= '7') {
		return "974" + phone
	}

	// البحرين: يبدأ بـ 3 وطوله 8
	if strings.HasPrefix(phone, "3") && len(phone) == 8 {
		return "973" + phone
	}

	// الأردن: يبدأ بـ 07 وطوله 10
	if strings.HasPrefix(phone, "07") && len(phone) == 10 {
		return "962" + phone[1:]
	}

	// لو مقدرناش نخمن → نرجعه زي ما هو
	return phone
}

// إرسال رسالة نصية عادية
func SendWAMessage(phone string, message string) error {
	if WAClient == nil || !WAClient.IsConnected() {
		return fmt.Errorf("حساب الواتساب غير متصل")
	}

	phone = normalizePhone(phone)
	if len(phone) < 8 {
		return fmt.Errorf("رقم غير صالح: %s", phone)
	}

	jid := types.NewJID(phone, types.DefaultUserServer)
	fmt.Printf("📤 جاري الإرسال النصي إلى: %s\n", jid.String())

	req := &waProto.Message{Conversation: proto.String(message)}
	_, err := WAClient.SendMessage(context.Background(), jid, req)
	if err != nil {
		fmt.Printf("❌ فشل الإرسال إلى %s: %v\n", phone, err)
		return err
	}

	fmt.Printf("✅ تم الإرسال بنجاح إلى: %s\n", phone)
	return nil
}

// إرسال صورة مع تعليق (Caption)
func SendWAImage(phone string, imageData []byte, caption string) error {
	if WAClient == nil || !WAClient.IsConnected() {
		return fmt.Errorf("حساب الواتساب غير متصل")
	}

	uploaded, err := WAClient.Upload(context.Background(), imageData, whatsmeow.MediaImage)
	if err != nil {
		return fmt.Errorf("فشل رفع الصورة: %v", err)
	}

	phone = normalizePhone(phone)
	jid := types.NewJID(phone, types.DefaultUserServer)

	mimeType := http.DetectContentType(imageData)
	if mimeType == "application/octet-stream" {
		mimeType = "image/jpeg"
	}

	msg := &waProto.Message{
		ImageMessage: &waProto.ImageMessage{
			Caption:       proto.String(caption),
			Mimetype:      proto.String(mimeType),
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(imageData))),
		},
	}

	_, err = WAClient.SendMessage(context.Background(), jid, msg)
	if err != nil {
		fmt.Printf("❌ فشل إرسال الصورة إلى %s: %v\n", phone, err)
		return err
	}

	fmt.Printf("✅ تم إرسال الصورة بنجاح إلى: %s\n", phone)
	return nil
}

// إرسال ملف PDF أو أي مستند
func SendWADocument(phone string, fileData []byte, fileName string, caption string) error {
	if WAClient == nil || !WAClient.IsConnected() {
		return fmt.Errorf("حساب الواتساب غير متصل")
	}

	uploaded, err := WAClient.Upload(context.Background(), fileData, whatsmeow.MediaDocument)
	if err != nil {
		return fmt.Errorf("فشل رفع الملف: %v", err)
	}

	phone = normalizePhone(phone)
	jid := types.NewJID(phone, types.DefaultUserServer)

	mimeType := http.DetectContentType(fileData)
	if strings.HasSuffix(strings.ToLower(fileName), ".pdf") {
		mimeType = "application/pdf"
	}

	msg := &waProto.Message{
		DocumentMessage: &waProto.DocumentMessage{
			Title:         proto.String(fileName),
			FileName:      proto.String(fileName),
			Caption:       proto.String(caption),
			Mimetype:      proto.String(mimeType),
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(fileData))),
		},
	}

	_, err = WAClient.SendMessage(context.Background(), jid, msg)
	if err != nil {
		fmt.Printf("❌ فشل إرسال الملف إلى %s: %v\n", phone, err)
		return err
	}

	fmt.Printf("✅ تم إرسال الملف بنجاح إلى: %s\n", phone)
	return nil
}

// دالة الإرسال الجماعي (تدعم نص + صورة/PDF)
func BroadcastWhatsAppHandler(c *gin.Context) {
	messageText := c.PostForm("message_text")
	if strings.TrimSpace(messageText) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "نص الرسالة مطلوب"})
		return
	}

	if WAClient == nil || !WAClient.IsConnected() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "الواتساب غير متصل"})
		return
	}

	// قراءة الملف المرفق مباشرة في الذاكرة (بدون حفظ على الديسك)
	var mediaData []byte
	var mediaFileName string
	var mediaType string

	fileHeader, err := c.FormFile("media")
	if err == nil && fileHeader != nil {
		file, err := fileHeader.Open()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "فشل فتح الملف المرفق"})
			return
		}
		defer file.Close()

		mediaData, err = io.ReadAll(file)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "فشل قراءة الملف المرفق"})
			return
		}

		mediaFileName = fileHeader.Filename

		ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
		if ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp" || ext == ".gif" {
			mediaType = "image"
		} else {
			mediaType = "document"
		}

		if len(mediaData) > 10*1024*1024 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "حجم الملف كبير جداً (الحد الأقصى 10 ميجا)"})
			return
		}
	}

	var guests []Guest
	DB.Find(&guests)

	// تحديد الدومين تلقائياً حسب السيرفر الحالي
	scheme := "http"
	//if c.Request.TLS != nil || c.Request.Header.Get("X-Forwarded-Proto") == "https" {
		//scheme = "https"
	//}
	baseURL := fmt.Sprintf("%s://%s", scheme, c.Request.Host)

	var wg sync.WaitGroup
	var successCount, failCount int
	var mu sync.Mutex

	for _, guest := range guests {
		if strings.TrimSpace(guest.Phone) == "" {
			continue
		}
		wg.Add(1)
		go func(g Guest) {
			defer wg.Done()

			// رابط الدعوة الخاص بالضيف (يتغير حسب السيرفر)
			inviteLink := fmt.Sprintf("%s/invite/%s", baseURL, g.Token)

			// استبدال المتغيرات
			fullMessage := strings.ReplaceAll(messageText, "{name}", g.Name)
			fullMessage = strings.ReplaceAll(fullMessage, "{link}", inviteLink)

			var sendErr error
			if len(mediaData) > 0 {
				if mediaType == "image" {
					sendErr = SendWAImage(g.Phone, mediaData, fullMessage)
				} else {
					sendErr = SendWADocument(g.Phone, mediaData, mediaFileName, fullMessage)
				}
			} else {
				sendErr = SendWAMessage(g.Phone, fullMessage)
			}

			mu.Lock()
			if sendErr != nil {
				failCount++
				fmt.Printf("❌ فشل إرسال لـ %s (%s): %v\n", g.Name, g.Phone, sendErr)
			} else {
				successCount++
				fmt.Printf("✅ تم الإرسال لـ %s\n", g.Name)
			}
			mu.Unlock()
		}(guest)
	}
	wg.Wait()

	c.JSON(http.StatusOK, gin.H{
		"message":       fmt.Sprintf("تم الإرسال: %d نجح، %d فشل", successCount, failCount),
		"success_count": successCount,
		"fail_count":    failCount,
	})
}

// فصل الحساب الحالي وإعادة التهيئة للربط من جديد
func LogoutWhatsAppHandler(c *gin.Context) {
	if WAClient == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "لا يوجد عميل واتساب"})
		return
	}

	// 1. تسجيل الخروج من سيرفرات واتساب (لو متصل)
	if WAClient.IsConnected() {
		err := WAClient.Logout(context.Background())
		if err != nil {
			fmt.Printf("⚠️ تحذير أثناء تسجيل الخروج: %v\n", err)
		}
		WAClient.Disconnect()
	}

	// 2. مسح بيانات الجلسة من الذاكرة
	qrMutex.Lock()
	CurrentQRBase64 = ""
	isConnecting = false
	qrMutex.Unlock()

	// 3. حذف ملف قاعدة بيانات الجلسة
	os.Remove("wa_store.db")
	os.Remove("wa_store.db-journal")
	os.Remove("wa_store.db-wal")
	os.Remove("wa_store.db-shm")

	// 4. إعادة تهيئة العميل من الصفر
	WAClient = nil
	InitWhatsApp()

	fmt.Println("🔄 تم فصل الحساب بنجاح. جاهز للربط بحساب جديد.")
	c.JSON(http.StatusOK, gin.H{
		"message": "تم فصل الحساب بنجاح. يمكنك الآن مسح QR جديد بحساب آخر.",
	})
}
