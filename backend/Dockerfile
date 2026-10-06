# Образ landlord-backend: бинарники server, worker, cli; миграции встроены (docs/spec/18, раздел 2).
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/cli ./cmd/cli

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/ /app/
EXPOSE 8080
CMD ["/app/server"]
