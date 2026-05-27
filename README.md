# Self-Care — Бэкенд

REST API для приложения Self-Care — трекера рабочего стресса с AI-поддержкой в реальном времени и еженедельными инсайтами на базе [DeepSeek](https://deepseek.com) через OpenRouter.

## Технологический стек

- **Go** — стандартная библиотека + роутер [chi](https://github.com/go-chi/chi)
- **PostgreSQL** — через драйвер [pgx](https://github.com/jackc/pgx)
- **JWT** — stateless-аутентификация с версионированием токенов для инвалидации при logout
- **OpenRouter / DeepSeek** — стриминговые AI-ответы для функции Live Response
- **Docker** — контейнеризированный деплой
- **Swagger / OpenAPI** — автогенерируемая документация API

## Функциональность

- **Аутентификация** — регистрация и вход по email/паролю, JWT-токены, инвалидация через `token_version`
- **Трекинг настроения** — ежедневная запись уровня стресса, рабочих часов и тегов событий
- **Live Response** — стриминговый AI-ответ на стресс пользователя в реальном времени (DeepSeek через OpenRouter)
- **Недельная карточка** — автогенерируемое еженедельное резюме: главный стрессор, динамика стресса, лучший совет
- **Анализ и инсайты** — AI-инсайты за несколько рабочих недель
- **Уведомления** — планирование push-уведомлений для ежедневных напоминаний и недельных отчётов
- **Подписки** — премиум-уровень с квотами на LR-сессии; вебхук Prodamus для платёжных событий
- **Администрирование** — внутренний эндпоинт для управления пользователями

## Структура проекта

```
cmd/api/          — точка входа
internal/
  auth/           — регистрация, вход, JWT
  mood/           — записи стресса, часов, тегов
  liveresponse/   — стриминговая AI-сессия поддержки
  analysis/       — еженедельные AI-инсайты
  weeklycard/     — генерация недельной карточки
  subscription/   — премиум-уровень и квоты LR
  notification/   — планирование push-уведомлений
  payments/       — обработчик вебхука Prodamus
  user/           — профиль пользователя
  admin/          — административные эндпоинты
  personalinsights/ — генерация персонализированных инсайтов
  migrate/        — встроенная миграция схемы (schema.sql)
pkg/
  database/       — настройка pgxpool
  jwtutil/        — подпись и верификация JWT, middleware
  middleware/     — логирование ошибок, восстановление после паники
  respond/        — единые хелперы HTTP-ответов
  testutil/       — тестовая БД через testcontainers
db/migrations/    — устаревшие файлы миграций (не используются в рантайме)
docs/             — Swagger/OpenAPI спецификация
k8s/              — Kubernetes-манифесты
```

## Быстрый старт

### Требования

- Go 1.23+
- Docker и Docker Compose
- PostgreSQL 16 (или используй compose-файл)

### Локальная разработка

```bash
# Скопируй и настрой переменные окружения
cp .env.example .env

# Запусти PostgreSQL
docker compose up -d postgres

# Запусти API
go run ./cmd/api
```

### Переменные окружения

| Переменная | Описание | Пример |
|---|---|---|
| `DATABASE_URL` | Строка подключения к PostgreSQL | `postgres://user:pass@localhost:5432/selfcare?sslmode=disable` |
| `JWT_SECRET` | Секрет для подписи JWT (минимум 32 символа) | — |
| `OPENROUTER_API_KEY` | API-ключ OpenRouter для доступа к DeepSeek | — |
| `PORT` | Порт HTTP-сервера | `8080` |
| `APP_ENV` | `production` или `preview` (preview включает стек-трейсы в ответах) | `production` |
| `POSTGRES_PASSWORD` | Пароль суперпользователя Postgres (только Docker) | — |

### Запуск тестов

```bash
# Требует Docker (используются testcontainers с реальной БД)
go test ./... -race -count=1
```

### Документация API

[Scalar](https://scalar.com) UI доступен по адресу `/docs` при запущенном сервере. Исходная спецификация (OpenAPI JSON) — на `/docs/openapi.json` и в файле `docs/swagger.json`.

## Деплой

CI/CD через GitHub Actions (`.github/workflows/deploy.yml`):

1. **test** — запуск `go test ./... -race`
2. **lint** — `golangci-lint`
3. **build** — сборка и публикация Docker-образа в `ghcr.io/meiumo/self-care-backend/api`
4. **deploy** — SSH-деплой через репозиторий `deploy` (Ansible)

## Лицензия

MIT
