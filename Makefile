DATABASE_URL      ?= postgres://voca:voca@localhost:5432/voca?sslmode=disable
TEST_DATABASE_URL ?= postgres://voca:voca@localhost:5432/voca_test?sslmode=disable

.PHONY: db-up db-down test run vet

db-up:
	docker compose up -d --wait postgres

db-down:
	docker compose down

test:
	cd backend && TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -p 1 ./...  # -p 1: DB-backed packages share one test database

vet:
	cd backend && go vet ./...

run:
	cd backend && DATABASE_URL="$(DATABASE_URL)" go run ./cmd/api
