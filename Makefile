# build script

run: main
	./main

# docker run with .env file
test: image
	docker run -p 8080:8080 --env-file .env -it webdl

image:
	docker build -t webdl .

main: main.go main.html
	go get ./...
	go build -o ./main
