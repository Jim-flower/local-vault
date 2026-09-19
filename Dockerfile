# syntax=docker/dockerfile:1

FROM node:22-alpine AS frontend-build
WORKDIR /src/frontend

COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund

COPY frontend/ ./
ARG VITE_BASE_PATH=/
ENV VITE_BASE_PATH=${VITE_BASE_PATH}
RUN npm run build

FROM golang:1.25-alpine AS go-build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
COPY --from=frontend-build /src/frontend/dist ./frontend/dist

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/devhub .

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 devhub \
    && adduser -S -D -H -u 10001 -G devhub devhub \
    && mkdir -p /data /projects \
    && chown -R devhub:devhub /data /projects

COPY --from=go-build /out/devhub /usr/local/bin/devhub

USER devhub
WORKDIR /data
ENV HOME=/data

EXPOSE 8787 8788
VOLUME ["/data", "/projects"]

ENTRYPOINT ["/usr/local/bin/devhub"]
CMD ["-web", "-host", "0.0.0.0", "-port", "8787", "-remote-vault-port", "8788", "-no-open", "-allow-remote"]
