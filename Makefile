##################
# Docker compose
##################

dc_build:
	docker compose -f ./docker-compose.yml build

dc_start:
	docker compose -f ./docker-compose.yml start

dc_stop:
	docker compose -f ./docker-compose.yml stop

dc_restart:
	docker compose -f ./docker-compose.yml restart

dc_up:
	docker compose -f ./docker-compose.yml up -d --remove-orphans

dc_ps:
	docker compose -f ./docker-compose.yml ps

dc_logs:
	docker compose -f ./docker-compose.yml logs -f

dc_down:
	docker compose -f ./docker-compose.yml down -v --rmi=all --remove-orphans


##################
# Tests
##################

test:
	go test ./... -count=1 -race -cover

lint:
	golangci-lint run ./...


##################
# CLI
##################

cli_help:
	go run . help

cli_export:
	go run . export $(ARGS)

cli_delete:
	go run . delete $(ARGS)

cli_backup:
	go run . backup $(ARGS)

cli_health:
	go run . health

