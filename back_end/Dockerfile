# syntax=docker/dockerfile:1

FROM golang:1.27-alpine AS build
WORKDIR /src
# Зависимости отдельным слоем: они меняются реже кода.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/api-server ./cmd/api-server && \
    go build -trimpath -ldflags="-s -w" -o /out/report ./cmd/report

FROM alpine:3.21
# ca-certificates — для TLS к Telegram и Google.
# tzdata — обязательно: config зовёт time.LoadLocation, и без базы поясов
# это падает в рантайме, а не на сборке.
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 app

# WORKDIR /app, и сюда же монтируются config.yaml и data/. Так относительные
# пути из конфига (data/session.json) одинаково работают на хосте
# и в контейнере, и второй конфиг не нужен.
WORKDIR /app
# Каталог под сессию/токены создаём и отдаём app заранее: это закрывает случай
# именованного тома (Docker создаст его от root). Для бинд-монта (как в
# compose) это не поможет — там права диктует хост, см. README.
RUN mkdir -p /app/data && chown app:app /app/data
COPY --from=build /out/api-server /out/report /app/
USER app
EXPOSE 8080
CMD ["/app/api-server"]
