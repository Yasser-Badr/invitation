package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func cloudToken() string {
	return strings.TrimSpace(os.Getenv("WA_CLOUD_TOKEN"))
}

func cloudPhoneNumberID() string {
	return strings.TrimSpace(os.Getenv("WA_PHONE_NUMBER_ID"))
}

func cloudAPIVersion() string {
	v := strings.TrimSpace(os.Getenv("WA_CLOUD_API_VERSION"))
	if v == "" {
		return "v21.0"
	}
	return v
}

func cloudBaseURL() string {
	return fmt.Sprintf("https://graph.facebook.com/%s/%s", cloudAPIVersion(), cloudPhoneNumberID())
}

func cloudSend(payload map[string]interface{}) error {
	token := cloudToken()
	phoneID := cloudPhoneNumberID()
	if token == "" || phoneID == "" {
		return fmt.Errorf("WA_CLOUD_TOKEN أو WA_PHONE_NUMBER_ID غير مضبوط")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := cloudBaseURL() + "/messages"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	respBody, _ := io.ReadAll(res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("cloud api error %d: %s", res.StatusCode, string(respBody))
	}

	fmt.Printf("✅ Cloud API response: %s\n", string(respBody))
	return nil
}

func cloudNormalizePhone(phone string) string {
	phone = strings.TrimSpace(phone)
	phone = strings.ReplaceAll(phone, "+", "")
	phone = strings.ReplaceAll(phone, " ", "")
	phone = strings.ReplaceAll(phone, "-", "")
	if n := normalizePhone(phone); n != "" {
		return n
	}
	return phone
}

func CloudSendText(to string, message string) error {
	to = cloudNormalizePhone(to)
	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                to,
		"type":              "text",
		"text": map[string]interface{}{
			"preview_url": false,
			"body":        message,
		},
	}
	return cloudSend(payload)
}

func CloudSendImageByURL(to string, imageURL string, caption string) error {
	to = cloudNormalizePhone(to)
	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "image",
		"image": map[string]interface{}{
			"link":    imageURL,
			"caption": caption,
		},
	}
	return cloudSend(payload)
}

func CloudSendVideoByURL(to string, videoURL string, caption string) error {
	to = cloudNormalizePhone(to)
	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "video",
		"video": map[string]interface{}{
			"link":    videoURL,
			"caption": caption,
		},
	}
	return cloudSend(payload)
}

func CloudSendTemplate(to string, templateName string, languageCode string, bodyParams []string) error {
	to = cloudNormalizePhone(to)
	if languageCode == "" {
		languageCode = "ar"
	}

	var params []map[string]interface{}
	for _, p := range bodyParams {
		params = append(params, map[string]interface{}{
			"type": "text",
			"text": p,
		})
	}

	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "template",
		"template": map[string]interface{}{
			"name": templateName,
			"language": map[string]interface{}{
				"code": languageCode,
			},
			"components": []map[string]interface{}{
				{
					"type":       "body",
					"parameters": params,
				},
			},
		},
	}
	return cloudSend(payload)
}

func CloudTestSendHandler(c *gin.Context) {
	phone := c.PostForm("phone")
	msg := c.PostForm("message")
	mode := c.PostForm("mode")

	if strings.TrimSpace(phone) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "رقم الهاتف مطلوب"})
		return
	}
	if strings.TrimSpace(msg) == "" {
		msg = "مرحباً! هذه رسالة تجريبية من نظام الدعوات."
	}

	var err error
	if mode == "buttons" {
		err = CloudSendButtons(phone, msg)
	} else {
		err = CloudSendText(phone, msg)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "تم الإرسال عبر Cloud API"})
}

func CloudWebhookVerifyHandler(c *gin.Context) {
	mode := c.Query("hub.mode")
	token := c.Query("hub.verify_token")
	challenge := c.Query("hub.challenge")

	verifyToken := os.Getenv("WA_WEBHOOK_VERIFY_TOKEN")
	if verifyToken == "" {
		verifyToken = "invite_verify_token"
	}

	if mode == "subscribe" && token == verifyToken {
		c.String(http.StatusOK, challenge)
		return
	}
	c.JSON(http.StatusForbidden, gin.H{"error": "verification failed"})
}

func CloudWebhookReceiveHandler(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad body"})
		return
	}
	fmt.Printf("📩 Webhook: %s\n", string(body))

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		c.Status(http.StatusOK)
		return
	}

	entries, _ := payload["entry"].([]interface{})
	for _, e := range entries {
		em, _ := e.(map[string]interface{})
		changes, _ := em["changes"].([]interface{})
		for _, ch := range changes {
			cm, _ := ch.(map[string]interface{})
			value, _ := cm["value"].(map[string]interface{})
			messages, _ := value["messages"].([]interface{})
			for _, m := range messages {
				msg, _ := m.(map[string]interface{})
				from, _ := msg["from"].(string)
				msgType, _ := msg["type"].(string)

				btnID := ""
				text := ""

				if msgType == "button" {
					button, _ := msg["button"].(map[string]interface{})
					btnID, _ = button["payload"].(string)
					if btnID == "" {
						btnID, _ = button["text"].(string)
					}
				}
				if msgType == "interactive" {
					inter, _ := msg["interactive"].(map[string]interface{})
					if inter != nil {
						if br, ok := inter["button_reply"].(map[string]interface{}); ok {
							btnID, _ = br["id"].(string)
							if t, ok := br["title"].(string); ok {
								text = t
							}
						}
					}
				}
				if msgType == "text" {
					t, _ := msg["text"].(map[string]interface{})
					text, _ = t["body"].(string)
				}

				textNorm := strings.ToLower(strings.TrimSpace(text))
				action := ""
				switch {
				case btnID == "confirm", textNorm == "تأكيد", textNorm == "تاكيد", textNorm == "1":
					action = "confirm"
				case btnID == "decline", textNorm == "اعتذار", textNorm == "2":
					action = "decline"
				case btnID == "location", textNorm == "لوكيشن 📍", strings.Contains(textNorm, "موقع"):
					action = "location"
				}
				if action == "" || from == "" {
					continue
				}

				guest, ok := findGuestByPhone(from)
				if !ok {
					fmt.Printf("⚠️ Cloud webhook: رقم غير مسجل %s\n", from)
					continue
				}

				switch action {
				case "confirm":
					processConfirmAttendance(guest)
				case "decline":
					processDeclineAttendance(guest)
				case "location":
					settings := getSettings()
					name := settings.LocationName
					if name == "" {
						name = "موقع القاعة"
					}
					address := settings.LocationAddress
					mapsURL := strings.TrimSpace(settings.MapsURL)

					var lat, lng float64
					sent := false
					if mapsURL != "" {
						if n, err := fmt.Sscanf(mapsURL, "https://maps.google.com/?q=%f,%f", &lat, &lng); err == nil && n == 2 {
							_ = CloudSendLocation(from, lat, lng, name, address)
							sent = true
						} else if n, err := fmt.Sscanf(mapsURL, "https://www.google.com/maps?q=%f,%f", &lat, &lng); err == nil && n == 2 {
							_ = CloudSendLocation(from, lat, lng, name, address)
							sent = true
						}
					}
					if !sent {
						if mapsURL != "" {
							bodyText := name
							if address != "" {
								bodyText += "\n" + address
							}
							_ = CloudSendLocationLink(from, mapsURL, bodyText)
						} else {
							_ = CloudSendText(from, "سيتم إرسال الموقع قريباً.")
						}
					}
				}
			}
		}
	}

	c.Status(http.StatusOK)
}

func adminWhatsAppURL() string {
	phone := strings.TrimSpace(os.Getenv("WA_ADMIN_PHONE"))
	phone = strings.ReplaceAll(phone, "+", "")
	phone = strings.ReplaceAll(phone, " ", "")
	phone = strings.ReplaceAll(phone, "-", "")
	if phone == "" {
		return ""
	}
	return "https://wa.me/" + phone
}

func CloudSendInvite(to, body, imageURL string) error {
	to = cloudNormalizePhone(to)

	buttons := []map[string]interface{}{
		{
			"type": "reply",
			"reply": map[string]interface{}{
				"id":    "confirm",
				"title": "تأكيد",
			},
		},
		{
			"type": "reply",
			"reply": map[string]interface{}{
				"id":    "decline",
				"title": "اعتذار",
			},
		},
	}

	interactive := map[string]interface{}{
		"type": "button",
		"body": map[string]interface{}{
			"text": body,
		},
		"action": map[string]interface{}{
			"buttons": buttons,
		},
	}

	if strings.TrimSpace(imageURL) != "" {
		interactive["header"] = map[string]interface{}{
			"type": "image",
			"image": map[string]interface{}{
				"link": imageURL,
			},
		}
	}

	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                to,
		"type":              "interactive",
		"interactive":       interactive,
	}
	return cloudSend(payload)
}

func CloudSendButtons(to string, body string) error {
	return CloudSendInvite(to, body, "")
}

func CloudSendQRWithLocation(to, qrImageURL, body, mapsURL string) error {
	to = cloudNormalizePhone(to)

	if strings.TrimSpace(mapsURL) == "" {
		return CloudSendImageByURL(to, qrImageURL, body)
	}

	interactive := map[string]interface{}{
		"type": "cta_url",
		"body": map[string]interface{}{
			"text": body,
		},
		"action": map[string]interface{}{
			"name": "cta_url",
			"parameters": map[string]interface{}{
				"display_text": "📍 فتح اللوكيشن",
				"url":          mapsURL,
			},
		},
	}

	if strings.TrimSpace(qrImageURL) != "" {
		interactive["header"] = map[string]interface{}{
			"type": "image",
			"image": map[string]interface{}{
				"link": qrImageURL,
			},
		}
	}

	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                to,
		"type":              "interactive",
		"interactive":       interactive,
	}
	return cloudSend(payload)
}

func CloudSendContactAdmin(to, body string) error {
	to = cloudNormalizePhone(to)
	waURL := adminWhatsAppURL()
	if waURL == "" {
		if body == "" {
			body = "للتواصل مع الإدارة راسلنا على واتساب."
		}
		return CloudSendText(to, body)
	}
	if body == "" {
		body = "للتواصل مع الإدارة اضغط الزر 👇"
	}
	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                to,
		"type":              "interactive",
		"interactive": map[string]interface{}{
			"type": "cta_url",
			"body": map[string]interface{}{
				"text": body,
			},
			"action": map[string]interface{}{
				"name": "cta_url",
				"parameters": map[string]interface{}{
					"display_text": "واتساب الإدارة 💬",
					"url":          waURL,
				},
			},
		},
	}
	return cloudSend(payload)
}

func CloudSendLocation(to string, lat, lng float64, name, address string) error {
	to = cloudNormalizePhone(to)
	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "location",
		"location": map[string]interface{}{
			"latitude":  lat,
			"longitude": lng,
			"name":      name,
			"address":   address,
		},
	}
	return cloudSend(payload)
}

func CloudSendLocationLink(to string, mapsURL string, body string) error {
	to = cloudNormalizePhone(to)
	if body == "" {
		body = "اضغط الزر لفتح موقع القاعة على الخريطة 📍"
	}
	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                to,
		"type":              "interactive",
		"interactive": map[string]interface{}{
			"type": "cta_url",
			"body": map[string]interface{}{
				"text": body,
			},
			"action": map[string]interface{}{
				"name": "cta_url",
				"parameters": map[string]interface{}{
					"display_text": "فتح الموقع",
					"url":          mapsURL,
				},
			},
		},
	}
	return cloudSend(payload)
}

func BroadcastCloudHandler(c *gin.Context) {
	if cloudToken() == "" || cloudPhoneNumberID() == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cloud API غير مضبوط — راجع ملف .env"})
		return
	}

	messageText := c.PostForm("message_text")
	if strings.TrimSpace(messageText) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "نص الرسالة مطلوب"})
		return
	}

	sendMode := c.PostForm("send_mode")
	if sendMode == "" {
		sendMode = "buttons"
	}

	var mediaPublicURL string
	var mediaKind string // "image" | "video"

	fileHeader, err := c.FormFile("media")
	if err == nil && fileHeader != nil {
		ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
		switch ext {
		case ".jpg", ".jpeg", ".png", ".webp":
			mediaKind = "image"
			if fileHeader.Size > 5*1024*1024 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "حجم الصورة كبير (الحد 5 ميجا)"})
				return
			}
		case ".mp4", ".3gp":
			mediaKind = "video"
			if fileHeader.Size > 16*1024*1024 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "حجم الفيديو كبير (الحد 16 ميجا)"})
				return
			}
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "الصيغ المسموحة: jpg/png/webp أو mp4"})
			return
		}

		_ = os.MkdirAll("./public/uploads", 0o755)
		filename := fmt.Sprintf("broadcast_%d%s", time.Now().UnixNano(), ext)
		savePath := "./public/uploads/" + filename
		if err := c.SaveUploadedFile(fileHeader, savePath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "فشل حفظ الملف"})
			return
		}
		base := strings.TrimRight(getAppBaseURL(), "/")
		if base == "" {
			base = strings.TrimRight(os.Getenv("APP_BASE_URL"), "/")
		}
		mediaPublicURL = base + "/public/uploads/" + filename
		fmt.Printf("📎 مرفق البث (%s): %s\n", mediaKind, mediaPublicURL)
	}

	idsStr := c.PostForm("guest_ids")
	selectedOnly := c.PostForm("selected_only") == "1"

	var guests []Guest
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "لم يتم تحديد مدعوين صالحين"})
			return
		}
		DB.Where("id IN ?", ids).Find(&guests)
	} else {
		DB.Find(&guests)
	}
	if len(guests) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "لا يوجد مدعوين"})
		return
	}

	scheme := "http"
	if c.Request.TLS != nil || c.Request.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := c.Request.Host
	baseURL := fmt.Sprintf("%s://%s", scheme, host)

	type resultItem struct {
		ID    uint   `json:"id"`
		Name  string `json:"name"`
		Phone string `json:"phone"`
		Error string `json:"error,omitempty"`
	}
	var successList, failList []resultItem

	for _, g := range guests {
		if strings.TrimSpace(g.Phone) == "" {
			failList = append(failList, resultItem{ID: g.ID, Name: g.Name, Phone: g.Phone, Error: "رقم الهاتف فارغ"})
			continue
		}
		inviteLink := fmt.Sprintf("%s/invite/%s", baseURL, g.Token)
		fullMessage := strings.ReplaceAll(messageText, "{name}", g.Name)
		fullMessage = strings.ReplaceAll(fullMessage, "{link}", inviteLink)

		var sendErr error
		if sendMode == "buttons" {
			if mediaPublicURL != "" && mediaKind == "video" {
				sendErr = CloudSendVideoByURL(g.Phone, mediaPublicURL, fullMessage)
				if sendErr == nil {
					time.Sleep(900 * time.Millisecond)
					sendErr = CloudSendInvite(g.Phone, "للرد على الدعوة اختر:", "")
				}
			} else {
				imgURL := ""
				if mediaKind == "image" {
					imgURL = mediaPublicURL
				}
				sendErr = CloudSendInvite(g.Phone, fullMessage, imgURL)
			}
			if sendErr == nil && adminWhatsAppURL() != "" {
				time.Sleep(700 * time.Millisecond)
				_ = CloudSendContactAdmin(g.Phone, "للتواصل مع الإدارة:")
			}
		} else if mediaPublicURL != "" {
			if mediaKind == "video" {
				sendErr = CloudSendVideoByURL(g.Phone, mediaPublicURL, fullMessage)
			} else {
				sendErr = CloudSendImageByURL(g.Phone, mediaPublicURL, fullMessage)
			}
		} else {
			sendErr = CloudSendText(g.Phone, fullMessage)
		}

		if sendErr != nil {
			failList = append(failList, resultItem{
				ID: g.ID, Name: g.Name, Phone: g.Phone, Error: sendErr.Error(),
			})
			fmt.Printf("❌ Cloud فشل %s: %v\n", g.Name, sendErr)
		} else {
			successList = append(successList, resultItem{
				ID: g.ID, Name: g.Name, Phone: g.Phone,
			})
			now := kuwaitNow()
			DB.Model(&Guest{}).Where("id = ?", g.ID).Updates(map[string]interface{}{
			    "invite_sent":    true,
	            "invite_sent_at": now,
			})
			fmt.Printf("✅ Cloud نجح %s\n", g.Name)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message":       fmt.Sprintf("Cloud API: %d نجح، %d فشل", len(successList), len(failList)),
		"success_count": len(successList),
		"fail_count":    len(failList),
		"success_list":  successList,
		"fail_list":     failList,
		"via":           "cloud",
		"send_mode":     sendMode,
		"message_text":  messageText,
		"has_media":     mediaPublicURL != "",
	})
}