# One image: the built web games served by the Go API.

FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
COPY testdata/ /src/testdata/
RUN npm run build

FROM golang:1.23-alpine AS server
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/arcade ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server /out/arcade /arcade
COPY --from=web /src/web/dist /web
ENV ADDR=:8080 STATIC_DIR=/web
EXPOSE 8080
USER nonroot
ENTRYPOINT ["/arcade"]
