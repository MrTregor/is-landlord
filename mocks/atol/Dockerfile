FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mock .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/mock /app/mock
EXPOSE 8082
ENTRYPOINT ["/app/mock"]
