.PHONY: dev web build test check clean

dev:
	go run ./cmd/selfsend

web:
	cd frontend && npm ci && npm run build

build: web
	mkdir -p dist
	go build -trimpath -o dist/selfsend ./cmd/selfsend

test:
	go test ./...

check:
	cd frontend && npm run typecheck && npm run build
	test -z "$$(gofmt -l .)"
	go vet ./...
	go test ./...

clean:
	rm -r dist 2>/dev/null || true
