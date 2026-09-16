FROM golang:1.27.1 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /eve-trader .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /eve-trader /eve-trader
VOLUME ["/data"]
EXPOSE 8080
ENV EVE_TRADER_ADDR=:8080 EVE_TRADER_DB_PATH=/data/eve-trader.db
ENTRYPOINT ["/eve-trader"]
