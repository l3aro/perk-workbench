.PHONY: sqlite mysql postgres mongo

sqlite:
	go run ./cmd/perk-workbench demo/chinook-sqlite.db

mysql:
	docker compose --project-directory . -f demo/compose.yaml up -d mysql
	docker compose --project-directory . -f demo/compose.yaml run --rm -t dev go run ./cmd/perk-workbench 'mysql:root:root@tcp(mysql:3306)/office'

postgres:
	docker compose --project-directory . -f demo/compose.yaml up -d --wait postgres
	docker compose --project-directory . -f demo/compose.yaml exec -T postgres psql -U postgres -d postgres -c "ALTER ROLE postgres PASSWORD 'postgres'"
	docker compose --project-directory . -f demo/compose.yaml run --rm -t dev go run ./cmd/perk-workbench 'postgres://postgres:postgres@postgres:5432/employees?sslmode=disable'

mongo:
	docker compose --project-directory . -f demo/compose.yaml up -d --wait mongo
	docker compose --project-directory . -f demo/compose.yaml run --rm mongo-seed
	docker compose --project-directory . -f demo/compose.yaml run --rm -t dev go run ./cmd/perk-workbench 'mongodb://mongo:27017/atlas'