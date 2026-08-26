package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

var WAClient *whatsmeow.Client
var CurrentQRBase64 string
var qrMutex sync.Mutex
var isConnecting bool

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
	WAClient.AddEventHandler(handleIncomingWA)

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

func StartQRLogin() {
	qrMutex.Lock()
	if WAClient == nil {
		qrMutex.Unlock()
		return
	}
	if WAClient.IsConnected() && WAClient.Store.ID != nil {
		qrMutex.Unlock()
		return
	}
	if isConnecting {
		qrMutex.Unlock()
		return
	}
	isConnecting = true
	CurrentQRBase64 = ""
	qrMutex.Unlock()

	if WAClient.IsConnected() {
		WAClient.Disconnect()
		time.Sleep(500 * time.Millisecond)
	}

	qrChan, err := WAClient.GetQRChannel(context.Background())
	if err != nil {
		fmt.Printf("❌ فشل الحصول على قناة الـ QR: %v\n", err)
		qrMutex.Lock()
		isConnecting = false
		qrMutex.Unlock()
		return
	}

	err = WAClient.Connect()
	if err != nil {
		fmt.Printf("❌ فشل الاتصال بالواتساب: %v\n", err)
		qrMutex.Lock()
		isConnecting = false
		qrMutex.Unlock()
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

func WhatsAppStatusHandler(c *gin.Context) {
	cloudOK := cloudToken() != "" && cloudPhoneNumberID() != ""

	waOK := WAClient != nil && WAClient.IsConnected() && WAClient.Store.ID != nil
	if waOK {
		c.JSON(http.StatusOK, gin.H{
			"connected": true,
			"via":       "whatsmeow",
			"cloud_ok":  cloudOK,
		})
		return
	}

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
		"cloud_ok":  cloudOK,
	})
}

func normalizePhone(phone string) string {
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

	knownCodes := []string{
		"20", "966", "971", "965", "973", "974", "968",
		"962", "961", "963", "964", "218", "212", "213",
		"216", "249", "970", "967",
	}

	for _, code := range knownCodes {
		if strings.HasPrefix(phone, code) {
			return phone
		}
	}

	if strings.HasPrefix(phone, "01") && len(phone) == 11 {
		return "20" + phone[1:]
	}
	if strings.HasPrefix(phone, "1") && len(phone) == 10 {
		return "20" + phone
	}
	if strings.HasPrefix(phone, "05") && len(phone) == 10 {
		return "966" + phone[1:]
	}
	if strings.HasPrefix(phone, "5") && len(phone) == 9 {
		return "966" + phone
	}
	if len(phone) == 8 && (strings.HasPrefix(phone, "5") || strings.HasPrefix(phone, "6") || strings.HasPrefix(phone, "9")) {
		return "965" + phone
	}
	if len(phone) == 8 && phone[0] >= '3' && phone[0] <= '7' {
		return "974" + phone
	}
	if strings.HasPrefix(phone, "3") && len(phone) == 8 {
		return "973" + phone
	}
	if strings.HasPrefix(phone, "07") && len(phone) == 10 {
		return "962" + phone[1:]
	}

	return phone
}

func SendWAMessage(phone string, message string) error {
	if WAClient == nil || !WAClient.IsConnected() {
		return fmt.Errorf("حساب الواتساب غير متصل")
	}
	phone = normalizePhone(phone)
	if len(phone) < 8 {
		return fmt.Errorf("رقم غير صالح: %s", phone)
	}
	jid := types.NewJID(phone, types.DefaultUserServer)
	req := &waProto.Message{Conversation: proto.String(message)}
	_, err := WAClient.SendMessage(context.Background(), jid, req)
	if err != nil {
		fmt.Printf("❌ فشل الإرسال إلى %s: %v\n", phone, err)
		return err
	}
	fmt.Printf("✅ تم الإرسال بنجاح إلى: %s\n", phone)
	return nil
}

func toJPEG(data []byte) ([]byte, int, int, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, err
	}
	b := img.Bounds()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		return nil, 0, 0, err
	}
	return buf.Bytes(), b.Dx(), b.Dy(), nil
}

func makeThumbnail(jpegData []byte) []byte {
	img, err := jpeg.Decode(bytes.NewReader(jpegData))
	if err != nil {
		img2, _, err2 := image.Decode(bytes.NewReader(jpegData))
		if err2 != nil {
			return nil
		}
		img = img2
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	tw := 72
	if w < tw {
		tw = w
	}
	th := h * tw / w
	if th < 1 {
		th = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	for y := 0; y < th; y++ {
		for x := 0; x < tw; x++ {
			sx := x * w / tw
			sy := y * h / th
			dst.Set(x, y, img.At(b.Min.X+sx, b.Min.Y+sy))
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 50}); err != nil {
		return nil
	}
	return buf.Bytes()
}

func SendWAImage(phone string, imageData []byte, caption string) error {
	if WAClient == nil || !WAClient.IsConnected() {
		return fmt.Errorf("حساب الواتساب غير متصل")
	}
	jpegData, width, height, err := toJPEG(imageData)
	if err != nil {
		jpegData = imageData
		width, height = 800, 800
		fmt.Printf("⚠️ تحذير تحويل الصورة: %v\n", err)
	}
	uploaded, err := WAClient.Upload(context.Background(), jpegData, whatsmeow.MediaImage)
	if err != nil {
		return fmt.Errorf("فشل رفع الصورة: %v", err)
	}
	phone = normalizePhone(phone)
	jid := types.NewJID(phone, types.DefaultUserServer)
	thumb := makeThumbnail(jpegData)
	imgMsg := &waProto.ImageMessage{
		Caption:       proto.String(caption),
		Mimetype:      proto.String("image/jpeg"),
		URL:           proto.String(uploaded.URL),
		DirectPath:    proto.String(uploaded.DirectPath),
		MediaKey:      uploaded.MediaKey,
		FileEncSHA256: uploaded.FileEncSHA256,
		FileSHA256:    uploaded.FileSHA256,
		FileLength:    proto.Uint64(uint64(len(jpegData))),
		Width:         proto.Uint32(uint32(width)),
		Height:        proto.Uint32(uint32(height)),
	}
	if len(thumb) > 0 {
		imgMsg.JPEGThumbnail = thumb
	}
	_, err = WAClient.SendMessage(context.Background(), jid, &waProto.Message{ImageMessage: imgMsg})
	if err != nil {
		fmt.Printf("❌ فشل إرسال الصورة إلى %s: %v\n", phone, err)
		return err
	}
	fmt.Printf("✅ تم إرسال الصورة بنجاح إلى: %s (%dx%d)\n", phone, width, height)
	return nil
}

func SendWAVideo(phone string, videoData []byte, caption string) error {
	if WAClient == nil || !WAClient.IsConnected() {
		return fmt.Errorf("حساب الواتساب غير متصل")
	}
	uploaded, err := WAClient.Upload(context.Background(), videoData, whatsmeow.MediaVideo)
	if err != nil {
		return fmt.Errorf("فشل رفع الفيديو: %v", err)
	}
	phone = normalizePhone(phone)
	jid := types.NewJID(phone, types.DefaultUserServer)
	msg := &waProto.Message{
		VideoMessage: &waProto.VideoMessage{
			Caption:       proto.String(caption),
			Mimetype:      proto.String("video/mp4"),
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(videoData))),
		},
	}
	_, err = WAClient.SendMessage(context.Background(), jid, msg)
	if err != nil {
		fmt.Printf("❌ فشل إرسال الفيديو إلى %s: %v\n", phone, err)
		return err
	}
	fmt.Printf("✅ تم إرسال الفيديو بنجاح إلى: %s\n", phone)
	return nil
}

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

func SendWAButtons(phone string, body string, footer string) error {
	if WAClient == nil || !WAClient.IsConnected() {
		return fmt.Errorf("حساب الواتساب غير متصل")
	}
	phone = normalizePhone(phone)
	if len(phone) < 8 {
		return fmt.Errorf("رقم غير صالح: %s", phone)
	}
	jid := types.NewJID(phone, types.DefaultUserServer)

	msg := &waProto.Message{
		ButtonsMessage: &waProto.ButtonsMessage{
			ContentText: proto.String(body),
			FooterText:  proto.String(footer),
			HeaderType:  waProto.ButtonsMessage_EMPTY.Enum(),
			Buttons: []*waProto.ButtonsMessage_Button{
				{
					ButtonID: proto.String("confirm"),
					ButtonText: &waProto.ButtonsMessage_Button_ButtonText{
						DisplayText: proto.String("تأكيد"),
					},
					Type: waProto.ButtonsMessage_Button_RESPONSE.Enum(),
				},
				{
					ButtonID: proto.String("decline"),
					ButtonText: &waProto.ButtonsMessage_Button_ButtonText{
						DisplayText: proto.String("اعتذار"),
					},
					Type: waProto.ButtonsMessage_Button_RESPONSE.Enum(),
				},
			},
		},
	}

	_, err := WAClient.SendMessage(context.Background(), jid, msg)
	if err != nil {
		fmt.Printf("⚠️ الأزرار لم تُرسل (%v) — إرسال نص بديل\n", err)
		fallback := body + "\n\n" +
			"للرد على الدعوة:\n" +
			"• اكتب: تأكيد\n" +
			"• أو اكتب: اعتذار\n\n" +
			footer
		return SendWAMessage(phone, fallback)
	}

	fmt.Printf("✅ تم إرسال رسالة الأزرار إلى: %s\n", phone)
	return nil
}

func findGuestByPhone(phone string) (*Guest, bool) {
	n := normalizePhone(phone)
	var guests []Guest
	DB.Find(&guests)
	for i := range guests {
		if normalizePhone(guests[i].Phone) == n {
			return &guests[i], true
		}
	}
	return nil, false
}

func getAppBaseURL() string {
	if v := os.Getenv("APP_BASE_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://invite.cloud-ip.cc"
}

// ===================== whatsapp.go =====================

func processConfirmAttendance(guest *Guest) {
	// منع التكرار
	if guest.Status == "confirmed" && guest.QRImageURL != "" {
		fmt.Printf("ℹ️ %s مؤكد مسبقاً — تجاهل تكرار التأكيد\n", guest.Name)
		return
	}

	guest.Status = "confirmed"
	baseURL := getAppBaseURL()
	verifyURL := fmt.Sprintf("%s/verify/%s", baseURL, guest.Token)
	qrFileName := fmt.Sprintf("%s.png", guest.Token)
	qrFilePath := fmt.Sprintf("./public/qrcodes/%s", qrFileName)
	_ = os.MkdirAll("./public/qrcodes", os.ModePerm)

	if err := qrcode.WriteFile(verifyURL, qrcode.Medium, 256, qrFilePath); err != nil {
		fmt.Printf("❌ فشل توليد QR لـ %s: %v\n", guest.Name, err)
		_ = CloudSendText(guest.Phone, "تم تسجيل تأكيد حضورك ✅\nحدث خطأ في إنشاء الباركود، تواصل مع المنظم.")
		DB.Save(guest)
		return
	}

	guest.QRImageURL = "/public/qrcodes/" + qrFileName
	DB.Save(guest)
	sendQRToGuest(guest)
}

func sendQRToGuest(guest *Guest) {
	path := "." + guest.QRImageURL
	data, err := os.ReadFile(path)
	if err != nil {
		msg := "تم تأكيد حضورك ✅\nتعذر إرسال الباركود حالياً."
		_ = CloudSendText(guest.Phone, msg)
		_ = SendWAMessage(guest.Phone, msg)
		return
	}

	companionsLine := "بدون مرافقين"
	if guest.Companions > 0 {
		companionsLine = fmt.Sprintf("%d", guest.Companions)
	}

	caption := fmt.Sprintf(
		"يا هلا بك يا %s، تم تأكيد حضورك بنجاح.\n\n"+
			"👥 | عدد المرافقين: %s\n\n"+
			"🎫 | الرجاء إظهار الباركود عند الدخول \n\n"+
			"يسعدنا تشريفكم 💚✨\n",
		guest.Name,
		companionsLine,
	)

	settings := getSettings()
	mapsURL := strings.TrimSpace(settings.MapsURL)
	imageURL := getAppBaseURL() + guest.QRImageURL

	if cloudToken() != "" && cloudPhoneNumberID() != "" {
		if err := CloudSendQRWithLocation(guest.Phone, imageURL, caption, mapsURL); err == nil {
			fmt.Printf("✅ باركود+موقع Cloud → %s\n", guest.Name)
			// _ = CloudSendContactAdmin(guest.Phone, "للتواصل مع الإدارة:")
			return
		}
		fmt.Printf("⚠️ Cloud QR+location: %v\n", err)
	}

	if err := SendWAImage(guest.Phone, data, caption); err != nil {
		_ = CloudSendText(guest.Phone, caption)
		_ = SendWAMessage(guest.Phone, caption+"\n(تعذر إرسال صورة الباركود)")
		return
	}
	fmt.Printf("✅ باركود whatsmeow → %s\n", guest.Name)
}

func processDeclineAttendance(guest *Guest) {
	if guest.QRImageURL != "" {
		_ = os.Remove("." + guest.QRImageURL)
		guest.QRImageURL = ""
	}
	guest.Status = "declined"
	// لا تغيّر CheckedIn ولا CheckedInAt
	DB.Save(guest)

	msg := fmt.Sprintf("تم تسجيل اعتذارك يا %s 🌸\nمقدرين ظروفك، ونتمنى نشوفك في مناسبات قادمة.", guest.Name)
	if cloudToken() != "" && cloudPhoneNumberID() != "" {
		_ = CloudSendText(guest.Phone, msg)
	} else {
		_ = SendWAMessage(guest.Phone, msg)
	}
	fmt.Printf("📝 اعتذار من %s — تم إلغاء الباركود (الدخول لم يُمسح)\n", guest.Name)
}

func handleIncomingWA(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		if v.Info.IsFromMe {
			return
		}

		phone := v.Info.Sender.User

		text := strings.TrimSpace(v.Message.GetConversation())
		if text == "" && v.Message.GetExtendedTextMessage() != nil {
			text = strings.TrimSpace(v.Message.GetExtendedTextMessage().GetText())
		}
		textNorm := strings.ToLower(text)

		btnID := ""
		if br := v.Message.GetButtonsResponseMessage(); br != nil {
			btnID = br.GetSelectedButtonID()
		}
		if tr := v.Message.GetTemplateButtonReplyMessage(); tr != nil {
			btnID = tr.GetSelectedID()
		}

		action := ""
		switch {
		case btnID == "confirm",
			textNorm == "تأكيد", textNorm == "تاكيد", textNorm == "1", textNorm == "confirm",
			strings.Contains(textNorm, "تأكيد الحضور"), strings.Contains(textNorm, "تاكيد الحضور"):
			action = "confirm"
		case btnID == "decline",
			textNorm == "اعتذار", textNorm == "2", textNorm == "decline",
			strings.Contains(textNorm, "اعتذار"):
			action = "decline"
		default:
			return
		}

		guest, ok := findGuestByPhone(phone)
		if !ok {
			fmt.Printf("⚠️ رسالة من رقم غير مسجل: %s | نص: %s | زر: %s\n", phone, text, btnID)
			return
		}

		if action == "confirm" {
			processConfirmAttendance(guest)
		} else {
			processDeclineAttendance(guest)
		}
	}
}

func BroadcastWhatsAppHandler(c *gin.Context) {
	messageText := c.PostForm("message_text")
	if strings.TrimSpace(messageText) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "نص الرسالة مطلوب"})
		return
	}
	if WAClient == nil || !WAClient.IsConnected() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "الواتساب (whatsmeow) غير متصل — امسح QR أولاً"})
		return
	}

	sendMode := c.PostForm("send_mode")
	if sendMode == "" {
		sendMode = "classic"
	}

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
		switch ext {
		case ".jpg", ".jpeg", ".png", ".webp", ".gif":
			mediaType = "image"
		case ".mp4", ".3gp":
			mediaType = "video"
		default:
			mediaType = "document"
		}
		if mediaType == "video" && len(mediaData) > 16*1024*1024 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "حجم الفيديو كبير (الحد 16 ميجا)"})
			return
		}
		if mediaType != "video" && len(mediaData) > 10*1024*1024 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "حجم الملف كبير جداً (الحد الأقصى 10 ميجا)"})
			return
		}
	}

	idsStr := c.PostForm("guest_ids")
	var guests []Guest
	if strings.TrimSpace(idsStr) != "" {
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "لم يتم تحديد مدعوين صالحين"})
			return
		}
		DB.Where("id IN ?", ids).Find(&guests)
	} else {
		DB.Find(&guests)
	}
	if len(guests) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "لا يوجد مدعوين للإرسال إليهم"})
		return
	}

	scheme := "http"
	if c.Request.TLS != nil || c.Request.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := c.Request.Host
	if strings.Contains(host, "cloud-ip.cc") {
		if idx := strings.Index(host, ":"); idx > 0 {
			host = host[:idx]
		}
	}
	baseURL := fmt.Sprintf("%s://%s", scheme, host)

	var wg sync.WaitGroup
	// var successCount, failCount int
	var mu sync.Mutex
	type resultItem struct {
		ID    uint   `json:"id"`
		Name  string `json:"name"`
		Phone string `json:"phone"`
		Error string `json:"error,omitempty"`
	}
	var successList, failList []resultItem

	for _, guest := range guests {
		if strings.TrimSpace(guest.Phone) == "" {
			continue
		}
		wg.Add(1)
		go func(g Guest) {
			defer wg.Done()
			inviteLink := fmt.Sprintf("%s/invite/%s", baseURL, g.Token)
			fullMessage := strings.ReplaceAll(messageText, "{name}", g.Name)
			fullMessage = strings.ReplaceAll(fullMessage, "{link}", inviteLink)
			
            var sendErr error
			if sendMode == "buttons" {
				if len(mediaData) > 0 {
					if mediaType == "image" {
						sendErr = SendWAImage(g.Phone, mediaData, fullMessage)
					} else if mediaType == "video" {
						sendErr = SendWAVideo(g.Phone, mediaData, fullMessage)
					} else {
						sendErr = SendWADocument(g.Phone, mediaData, mediaFileName, fullMessage)
					}
					if sendErr == nil {
						time.Sleep(500 * time.Millisecond)
						sendErr = SendWAButtons(g.Phone, "للرد على الدعوة اختر:", "أو اكتب: تأكيد / اعتذار")
					}
				} else {
					body := fullMessage + "\n\nاضغط أحد الزرين للرد:"
					sendErr = SendWAButtons(g.Phone, body, "أو اكتب: تأكيد / اعتذار")
				}
			} else {
				if len(mediaData) > 0 {
					if mediaType == "image" {
						sendErr = SendWAImage(g.Phone, mediaData, fullMessage)
					} else if mediaType == "video" {
						sendErr = SendWAVideo(g.Phone, mediaData, fullMessage)
					} else {
						sendErr = SendWADocument(g.Phone, mediaData, mediaFileName, fullMessage)
					}
				} else {
					sendErr = SendWAMessage(g.Phone, fullMessage)
				}
			}
mu.Lock()
			if sendErr != nil {
				failList = append(failList, resultItem{
					ID: g.ID, Name: g.Name, Phone: g.Phone, Error: sendErr.Error(),
				})
				fmt.Printf("❌ فشل إرسال لـ %s (%s): %v\n", g.Name, g.Phone, sendErr)
			} else {
				successList = append(successList, resultItem{
					ID: g.ID, Name: g.Name, Phone: g.Phone,
				})
				fmt.Printf("✅ تم الإرسال لـ %s [whatsmeow]\n", g.Name)
			}
			mu.Unlock()
		}(guest)
	}
	wg.Wait()

	c.JSON(http.StatusOK, gin.H{
		"message":       fmt.Sprintf("whatsmeow: %d نجح، %d فشل", len(successList), len(failList)),
		"success_count": len(successList),
		"fail_count":    len(failList),
		"success_list":  successList,
		"fail_list":     failList,
		"via":           "whatsmeow",
		"send_mode":     sendMode,
		"message_text":  messageText,
	})
}

func LogoutWhatsAppHandler(c *gin.Context) {
	if WAClient == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "لا يوجد عميل واتساب"})
		return
	}
	if WAClient.IsConnected() {
		err := WAClient.Logout(context.Background())
		if err != nil {
			fmt.Printf("⚠️ تحذير أثناء تسجيل الخروج: %v\n", err)
		}
		WAClient.Disconnect()
	}
	qrMutex.Lock()
	CurrentQRBase64 = ""
	isConnecting = false
	qrMutex.Unlock()
	os.Remove("wa_store.db")
	os.Remove("wa_store.db-journal")
	os.Remove("wa_store.db-wal")
	os.Remove("wa_store.db-shm")
	WAClient = nil
	InitWhatsApp()
	fmt.Println("🔄 تم فصل الحساب بنجاح. جاهز للربط بحساب جديد.")
	c.JSON(http.StatusOK, gin.H{"message": "تم فصل الحساب بنجاح. يمكنك الآن مسح QR جديد بحساب آخر."})
}

func sendLocationAfterConfirm(guest *Guest) {
	settings := getSettings()
	name := settings.LocationName
	if name == "" {
		name = "موقع القاعة"
	}
	address := settings.LocationAddress
	mapsURL := strings.TrimSpace(settings.MapsURL)

	var lat, lng float64
	if mapsURL != "" {
		if n, err := fmt.Sscanf(mapsURL, "https://maps.google.com/?q=%f,%f", &lat, &lng); err == nil && n == 2 {
			_ = CloudSendLocation(guest.Phone, lat, lng, name, address)
			return
		}
		if n, err := fmt.Sscanf(mapsURL, "https://www.google.com/maps?q=%f,%f", &lat, &lng); err == nil && n == 2 {
			_ = CloudSendLocation(guest.Phone, lat, lng, name, address)
			return
		}
	}

	if mapsURL != "" {
		body := name
		if address != "" {
			body += "\n" + address
		}
		_ = CloudSendLocationLink(guest.Phone, mapsURL, body)
	}
}
