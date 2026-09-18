# المرحلة الأولى: بناء المشروع
FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN mkdir -p public

RUN CGO_ENABLED=0 GOOS=linux go build -o wedding-app .

# المرحلة الثانية: التشغيل
FROM alpine:latest

# تثبيت بيانات التوقيت
RUN apk add --no-cache tzdata

WORKDIR /app

COPY --from=builder /app/wedding-app .
COPY --from=builder /app/templates ./templates
COPY --from=builder /app/public ./public

ENV PORT=8080
ENV TZ=Asia/Kuwait
EXPOSE 8080

CMD ["./wedding-app"]
