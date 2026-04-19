FROM golang:1.25-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/redixis ./cmd/redixis

FROM alpine:3.21

RUN apk add --no-cache ca-certificates && adduser -D -H -u 10001 redixis

USER redixis
WORKDIR /app

COPY --from=build /out/redixis /app/redixis

EXPOSE 8080

ENTRYPOINT ["/app/redixis"]
