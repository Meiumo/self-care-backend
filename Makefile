.PHONY: dev build test lint \
        docker-up docker-down docker-logs db-shell \
        mk-start mk-build mk-apply mk-up mk-down mk-url mk-logs mk-status mk-forward \
        tidy

# ─── Local dev ────────────────────────────────────────────────────────────────

dev:
	go run ./cmd/api

build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/api ./cmd/api

test:
	go test ./... -v -race -count=1

lint:
	golangci-lint run ./...

tidy:
	go mod tidy

# ─── Docker Compose (быстрый локальный запуск) ────────────────────────────────

docker-up:
	docker compose up --build -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f api

db-shell:
	docker compose exec db psql -U selfcare selfcare

# ─── Minikube ─────────────────────────────────────────────────────────────────

mk-start:
	minikube start --cpus 2 --memory 2048
	minikube addons enable ingress

# Собрать образ прямо внутри Minikube (не нужен registry)
mk-build:
	eval $$(minikube docker-env) && \
	docker build -t selfcare-api:local .

# Применить все манифесты
mk-apply:
	kubectl apply -f k8s/namespace.yaml
	kubectl apply -f k8s/secrets.yaml
	kubectl apply -f k8s/postgres-init-cm.yaml
	kubectl apply -f k8s/postgres.yaml
	kubectl apply -f k8s/api.yaml

# Полный цикл: собрать образ + задеплоить
mk-up: mk-build mk-apply

# Удалить всё в namespace selfcare
mk-down:
	kubectl delete namespace selfcare --ignore-not-found

# URL для доступа к API с хоста
mk-url:
	minikube service selfcare-api -n selfcare --url

# Логи API-пода
mk-logs:
	kubectl logs -n selfcare -l app=selfcare-api -f

# Состояние подов
mk-status:
	kubectl get pods,svc,pvc -n selfcare

# Пробросить порт API на все интерфейсы (нужно для доступа с телефона через VPN)
mk-forward:
	kubectl port-forward -n selfcare svc/selfcare-api 8080:80 --address 0.0.0.0

# Пересобрать и откатить деплой (быстрое обновление образа)
mk-redeploy: mk-build
	kubectl rollout restart deployment/selfcare-api -n selfcare
