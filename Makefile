.PHONY: sqlite mysql postgres mongo

sqlite:
	docker compose run --rm -t dev

mysql:
	docker compose up -d mysql
	docker compose run --rm -t dev go run ./cmd/perk-workbench 'mysql:root:root@tcp(mysql:3306)/office'

postgres:
	docker compose up -d --wait postgres
	docker compose exec -T postgres psql -U postgres -d postgres -c "ALTER ROLE postgres PASSWORD 'postgres'"
	docker compose run --rm -t dev go run ./cmd/perk-workbench 'postgres://postgres:postgres@postgres:5432/employees?sslmode=disable'

mongo:
	docker compose up -d --wait mongo
	docker compose run --rm mongo-seed
	docker compose run --rm -t dev go run ./cmd/perk-workbench 'mongodb://mongo:27017/atlas'
