# المرحلة الأولى: بناء المشروع
FROM golang:1.26-alpine AS builder

# تثبيت أدوات البناء لـ CGO (محتاجة لـ go-sqlite3)
RUN apk add --no-cache gcc musl-dev

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN mkdir -p public

# بناء مع تفعيل CGO
RUN CGO_ENABLED=1 GOOS=linux go build -o wedding-app .

# المرحلة الثانية: التشغيل
FROM alpine:latest

RUN apk add --no-cache tzdata ca-certificates

WORKDIR /app

COPY --from=builder /app/wedding-app .
COPY --from=builder /app/templates ./templates
COPY --from=builder /app/public ./public

ENV PORT=8080
ENV TZ=Asia/Kuwait
EXPOSE 8080

CMD ["./wedding-app"]