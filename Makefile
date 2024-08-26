# Define variáveis
DOCKER_COMPOSE_FILE = docker-compose.yml

.PHONY: build up down stop restart logs clean

# Builda as imagens Docker para a aplicação
build:
	docker-compose -f $(DOCKER_COMPOSE_FILE) build

# Sobe os containers da aplicação
up: build
	docker-compose -f $(DOCKER_COMPOSE_FILE) up -d

# Para e remove os containers, mas mantém os volumes
down:
	docker-compose -f $(DOCKER_COMPOSE_FILE) down

# Para os containers sem removê-los
stop:
	docker-compose -f $(DOCKER_COMPOSE_FILE) stop

# Reinicia os containers
restart: down up

# Mostra os logs de todos os containers
logs:
	docker-compose -f $(DOCKER_COMPOSE_FILE) logs -f

# Remove containers, redes, imagens e volumes
clean:
	docker-compose -f $(DOCKER_COMPOSE_FILE) down -v --rmi all --remove-orphans