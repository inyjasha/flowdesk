FROM node:24-alpine AS web
WORKDIR /web
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.25-alpine AS backend
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -o /flowdesk ./cmd/api
RUN CGO_ENABLED=0 go build -trimpath -o /seed ./cmd/seed

FROM golang:1.25-bookworm AS test
WORKDIR /src
COPY --from=backend /go/pkg/mod /go/pkg/mod
COPY backend/ ./
CMD ["go", "test", "-race", "-count=1", "-v", "./..."]

FROM alpine:3.22
RUN adduser -D -u 10001 app
WORKDIR /app
COPY --from=backend /flowdesk ./flowdesk
COPY --from=backend /seed ./seed
COPY --from=web /web/dist ./web
USER app
ENV STATIC_DIR=/app/web GIN_MODE=release HTTP_ADDR=:8090
EXPOSE 8090
CMD ["./flowdesk"]
