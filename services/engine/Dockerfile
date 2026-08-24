FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/engine ./cmd/api
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/outbox-publisher ./cmd/outbox-publisher
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/formal-execution-consumer ./cmd/formal-execution-consumer

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/engine /engine
COPY --from=build /out/outbox-publisher /outbox-publisher
COPY --from=build /out/formal-execution-consumer /formal-execution-consumer
EXPOSE 8080
ENTRYPOINT ["/engine"]
