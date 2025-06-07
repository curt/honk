FROM golang:1-alpine AS builder

RUN apk add --no-cache git sqlite sqlite-dev build-base
WORKDIR /app
COPY . .
RUN go build -o honk .

FROM alpine:latest

ENV ALLOW_HONK_ROOT=true
ENV HONK_DATADIR=/app/data
RUN apk add --no-cache sqlite sqlite-libs jq
WORKDIR /app
COPY --from=builder /app/honk .
COPY entry.sh .
COPY views views
RUN chmod +x entry.sh
VOLUME /app/data
EXPOSE 8000
CMD ["./entry.sh"]
