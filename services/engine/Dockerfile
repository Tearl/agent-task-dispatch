FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/engine ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/engine /engine
EXPOSE 8080
ENTRYPOINT ["/engine"]
