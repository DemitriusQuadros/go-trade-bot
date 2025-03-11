DOCKER_COMPOSE_FILE = docker-compose.yml

.PHONY: build up down stop restart logs clean

# Build docker images
build:
	docker-compose -f $(DOCKER_COMPOSE_FILE) build

# Up containers
up: build
	docker-compose -f $(DOCKER_COMPOSE_FILE) up -d

# Stop containers but keep volumes
down:
	docker-compose -f $(DOCKER_COMPOSE_FILE) down

# Stop containers without remove them
stop:
	docker-compose -f $(DOCKER_COMPOSE_FILE) stop

# Restart containers
restart: down up

# Show container logs
logs:
	docker-compose -f $(DOCKER_COMPOSE_FILE) logs -f

# Remove all containers and networks
clean:
	docker-compose -f $(DOCKER_COMPOSE_FILE) down -v --rmi all --remove-orphans

# run the api project locally
run-api-local:
	go run cmd/api/main.go

# run the worker project locally
run-worker-local:
	go run cmd/worker/main.go