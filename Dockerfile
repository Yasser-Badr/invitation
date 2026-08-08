# المرحلة الأولى: بناء المشروع (Build Stage)
FROM golang:1.22-alpine AS builder

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

# نسخ الملف التنفيذي والمجلدات الهامة (مثل templates و public) من مرحلة البناء
COPY --from=builder /app/wedding-app .
COPY --from=builder /app/templates ./templates
COPY --from=builder /app/public ./public

# Cloud Run يحدد المنفذ عبر متغير البيئة PORT (الافتراضي 8080)
ENV PORT=8080
EXPOSE 8080

# تشغيل التطبيق
CMD ["./wedding-app"]
