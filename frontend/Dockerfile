# Образ landlord-web: собранный фронтенд и Caddy (docs/spec/18 Развёртывание и эксплуатация.md, раздел 2).
# Контекст сборки — корень репозитория (нужен deploy/Caddyfile): docker build -f frontend/Dockerfile .
FROM node:22 AS build
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM caddy:2
COPY deploy/Caddyfile /etc/caddy/Caddyfile
COPY --from=build /src/dist/ /srv/
