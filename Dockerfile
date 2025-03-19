FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o main-amd64 . &&\
  CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o main-arm64 .

FROM alpine:latest

RUN apk update && apk add --no-cache ffmpeg ca-certificates
COPY --from=builder /app/main-* /app/
WORKDIR /app

RUN cd /app && ARCH=$(uname -m) && \
  if [ "$ARCH" = "x86_64" ]; then \
  YTDLP_URL="https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_linux" && \
  mv ./main-amd64 ./main && rm ./main-arm64; \
  elif [ "$ARCH" = "aarch64" ]; then \
  YTDLP_URL="https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_linux_aarch64" && \
  mv ./main-arm64 ./main && rm ./main-amd64; \
  else \
  echo "Unsupported architecture: $ARCH" && exit 1; \
  fi && \
  wget -qO ./yt-dlp "$YTDLP_URL" && \
  chmod a+x ./yt-dlp

EXPOSE 8080
# Run ytdlp -U and then run the application
CMD ["sh", "-c", "./yt-dlp -U && ./main"]
