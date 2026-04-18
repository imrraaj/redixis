FROM golang:1.25-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod tidy

COPY . .
RUN go build .

FROM alpine:3.21

RUN adduser -D -H -u 10001 redixis

USER redixis

COPY --from=build /src /src

EXPOSE 8080

ENTRYPOINT ["/src/redixis"]
