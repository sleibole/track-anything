.PHONY: run build test docker-build docker-run

run:
	go run .

build:
	CGO_ENABLED=0 go build -o bin/trackanything .

test:
	go test ./...

docker-build:
	docker build -t trackanything .

docker-run: docker-build
	docker run --rm -p 8080:8080 -v trackanything-data:/data trackanything
