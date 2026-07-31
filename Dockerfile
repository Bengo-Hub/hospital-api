# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN GOTOOLCHAIN=auto go mod download
COPY . .

# Build all binaries: api, migrate, and seed
RUN GOTOOLCHAIN=auto CGO_ENABLED=0 go build -o /out/hospital ./cmd/api
RUN GOTOOLCHAIN=auto CGO_ENABLED=0 go build -o /out/hospital-migrate ./cmd/migrate
RUN GOTOOLCHAIN=auto CGO_ENABLED=0 go build -o /out/hospital-seed ./cmd/seed

FROM alpine:3.20
RUN addgroup -S app && adduser -S app -G app
WORKDIR /app
COPY --from=builder /out/hospital /usr/local/bin/hospital
COPY --from=builder /out/hospital-migrate /usr/local/bin/hospital-migrate
COPY --from=builder /out/hospital-seed /usr/local/bin/hospital-seed
COPY scripts/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh
USER app
EXPOSE 4200
ENV PORT=4200
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
