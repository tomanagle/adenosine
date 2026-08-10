COMPOSE = docker compose --env-file .env.local -f dev/docker-compose.yml

.PHONY: dev dev-detached down reset logs test lint generate doctor shell psql e2e e2e-federation

dev:
	./scripts/dev.sh

dev-detached:
	./scripts/dev.sh --detach

down:
	$(COMPOSE) down

reset:
	./scripts/reset-dev.sh

logs:
	$(COMPOSE) logs -f

test:
	./scripts/test.sh

lint:
	./scripts/lint.sh

generate:
	./scripts/generate.sh

doctor:
	./scripts/doctor.sh

shell:
	$(COMPOSE) exec adenosine sh

psql:
	$(COMPOSE) exec postgres psql -U adenosine -d adenosine

e2e:
	./scripts/docker-task.sh e2e

e2e-federation:
	./scripts/docker-task.sh e2e-federation
