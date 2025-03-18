# build script

# docker run with .env file
test: image
	docker run -p 8080:8080 --env-file .env -it webdl

image: main
	docker build -t webdl .

main: main.go
	go get ./...
	go build


