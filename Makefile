.PHONY: build dev-api dev-web check seed

build:
	cd web && npm run build
	go build -o daytracker ./cmd/server

dev-api:
	go run -tags dev ./cmd/server

dev-web:
	cd web && npm run dev

check:
	go run -tags dev ./cmd/check

seed:
	go run ./cmd/seed
