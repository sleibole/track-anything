.PHONY: dev dev-agent dev-list dev-stop dev-stop-all run build test docker-build docker-run

# Personal server on port 8080. Not tracked by the dev-agent commands.
# Air is a developer tool, run by version so it is not a module dependency.
dev:
	go run github.com/air-verse/air@v1.67.4

dev-agent:
	@test -n "$(NAME)" || { echo "usage: make dev-agent NAME=<unique-name>" >&2; exit 1; }
	./scripts/dev-server.sh start "$(NAME)"

dev-list:
	./scripts/dev-server.sh list

dev-stop:
	@test -n "$(NAME)" || { echo "usage: make dev-stop NAME=<name>" >&2; exit 1; }
	./scripts/dev-server.sh stop "$(NAME)"

dev-stop-all:
	./scripts/dev-server.sh stop-all

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
