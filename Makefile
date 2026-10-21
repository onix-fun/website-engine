.PHONY: build test test-integration fmt vet compose-up compose-down

build:
	go build ./...

fmt:
	gofmt -w ./cmd ./internal ./migrations

vet:
	go vet ./...

test:
	go test ./...

test-integration:
	@test -n "$$TEST_DATABASE_URL" || (echo "Set TEST_DATABASE_URL to a disposable PostgreSQL database" && exit 1)
	go test -v ./internal/integration

compose-up:
	docker compose up --build

compose-down:
	docker compose down
