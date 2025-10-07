FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o main

FROM docker:28-dind AS runner

RUN apk add --no-cache bash git vim

WORKDIR /data
COPY --from=builder /app/main /app/main
EXPOSE 80

CMD ["/app/main"]

