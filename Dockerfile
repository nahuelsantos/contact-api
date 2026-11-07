FROM golang:1.26-alpine AS builder

ARG GIT_SHA=dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.sha=${GIT_SHA}" -o /out/contact-api ./cmd/contact-api

FROM alpine:3.21

RUN addgroup -S app && adduser -S -G app app
COPY --from=builder /out/contact-api /usr/local/bin/contact-api
COPY config/example.toml /etc/contact-api/forms.toml

USER app
EXPOSE 3002
HEALTHCHECK --interval=10s --timeout=3s --retries=3 CMD wget -qO- http://127.0.0.1:3002/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/contact-api"]
