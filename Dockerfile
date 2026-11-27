# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY go.mod ./
# Use wildcard so go.sum is copied automatically once added
COPY go.sum* ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/job ./cmd/job

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=builder /out/job /app/job
COPY prompts/ /app/prompts/

ENTRYPOINT ["/app/job"]
