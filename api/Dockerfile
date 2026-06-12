FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o persona-api .

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/persona-api .
EXPOSE 8080
ENV PORT=8080
CMD ["./persona-api"]
