# المرحلة الأولى: بناء المشروع (Build Stage)
FROM golang:1.26-alpine AS builder

WORKDIR /app

# تحميل الملفات الأساسية
COPY go.mod go.sum ./
RUN go mod download

# نسخ باقي ملفات المشروع
COPY . .

# بناء التطبيق كملف تنفيذى (Binary)
RUN CGO_ENABLED=0 GOOS=linux go build -o wedding-app .

# المرحلة الثانية: التشغيل النهائي (Runtime Stage)
FROM alpine:latest

WORKDIR /app

# نسخ الملف التنفيذي
COPY --from=builder /app/wedding-app .

# نسخ القوالب
COPY --from=builder /app/templates ./templates

# إنشاء مجلد public لو مش موجود + نسخه بأمان
RUN mkdir -p ./public
COPY --from=builder /app/public* ./public/ 2>/dev/null || true

# Cloud Run / المنفذ
ENV PORT=8080
EXPOSE 8080

CMD ["./wedding-app"]