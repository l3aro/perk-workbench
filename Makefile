.PHONY: sqlite mysql

sqlite:
	docker compose run --rm dev

mysql:
	docker compose up -d mysql
	docker compose run --rm dev go run ./cmd/perk 'mysql:root:root@tcp(mysql:3306)/office'
