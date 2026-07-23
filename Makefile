.PHONY: sqlite mysql postgres

sqlite:
	docker compose run --rm dev

mysql:
	docker compose up -d mysql
	docker compose run --rm dev go run ./cmd/perk 'mysql:root:root@tcp(mysql:3306)/office'

postgres:
	docker compose up -d postgres
	docker compose run --rm dev go run ./cmd/perk 'postgres://postgres:postgres@postgres:5432/employees?sslmode=disable'
