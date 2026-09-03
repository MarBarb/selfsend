.PHONY: dev web build test check ios-project ios-build clean

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

ios-project:
	cd ios/SelfSend && xcodegen generate

ios-build:
	cd ios/SelfSend && xcodebuild -project SelfSend.xcodeproj -scheme SelfSend -sdk iphonesimulator -destination 'generic/platform=iOS Simulator' CODE_SIGNING_ALLOWED=NO build

clean:
	rm -r dist 2>/dev/null || true
