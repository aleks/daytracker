.PHONY: build dev-api dev-web

build:
	cd web && npm run build
	go build -o daytracker ./cmd/server

dev-api:
	go run -tags dev ./cmd/server

dev-web:
	cd web && npm run dev
