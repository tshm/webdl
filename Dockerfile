FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o main-amd64 . &&\
  CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o main-arm64 .

FROM alpine:latest

RUN apk update && apk add --no-cache yt-dlp ffmpeg ca-certificates
COPY --from=builder /app/main-* /app/
WORKDIR /app

RUN cd /app && ARCH=$(uname -m) && \
  if [ "$ARCH" = "x86_64" ]; then \
  mv ./main-amd64 ./main && rm ./main-arm64; \
  elif [ "$ARCH" = "aarch64" ]; then \
  mv ./main-arm64 ./main && rm ./main-amd64; \
  else \
  echo "Unsupported architecture: $ARCH" && exit 1; \
  fi

EXPOSE 8080
# Run ytdlp -U and then run the application
CMD ["sh", "-c", "yt-dlp -U && ./main"]
