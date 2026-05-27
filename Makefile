.PHONY: build dev-api dev-web check seed test

build:
	cd web && npm run build
	go build -tags sqlite_fts5 -o daytracker ./cmd/server

dev-api:
	go run -tags "dev sqlite_fts5" ./cmd/server

dev-web:
	cd web && npm run dev

check:
	go run -tags "dev sqlite_fts5" ./cmd/check

seed:
	go run -tags sqlite_fts5 ./cmd/seed

test:
	go test -tags "dev sqlite_fts5" ./...
