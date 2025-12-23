FROM golang:1.23-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/streaming-service ./

FROM alpine:3.20
RUN adduser -D -g '' appuser
COPY --from=build /out/streaming-service /usr/local/bin/streaming-service
USER appuser
EXPOSE 8080
ENV LISTEN_ADDR=:8080
ENTRYPOINT ["/usr/local/bin/streaming-service"]
