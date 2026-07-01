.PHONY: build test run up down tidy

# Compile everything.
build:
	go build ./...

# Run the full test suite. Set TEST_DATABASE_URL to include the Postgres
# integration test in internal/store.
test:
	go test ./...

# Run the service directly (expects env from your shell or a local .env).
run:
	go run ./cmd/alerter

# Bring up the service plus Postgres with one command.
up:
	docker compose up --build

# Tear it down and drop the database volume.
down:
	docker compose down -v

tidy:
	go mod tidy
