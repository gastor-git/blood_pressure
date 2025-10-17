##################
# Docker compose
##################

dc_build:
	docker compose -f ./docker-compose.yml build

dc_start:
	docker compose -f ./docker-compose.yml start

dc_stop:
	docker compose -f ./docker-compose.yml stop

dc_up:
	docker compose -f ./docker-compose.yml up -d --remove-orphans

dc_ps:
	docker compose -f ./docker-compose.yml ps

dc_logs:
	docker compose -f ./docker-compose.yml logs -f

dc_down:
	docker compose -f ./docker-compose.yml down -v --rmi=all --remove-orphans


##################
# App
##################

app_bash:
	docker compose -f ./docker-compose.yml exec /bin/bash
