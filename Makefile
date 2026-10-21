.PHONY: dev-server dev-web test vectors build up

# API with no dependencies, scores kept in memory
dev-server:
	cd server && STORE=memory go run ./cmd/server

# web games with hot reload on http://localhost:5173, proxying /v1 to the API
dev-web:
	cd web && npm run dev

test:
	cd server && go vet ./... && go test -race ./...
	cd web && npm run typecheck && npm test

# regenerate the vectors both simulations are checked against
vectors:
	cd server && go run ./cmd/genvectors > ../testdata/vectors.json

build:
	cd web && npm run build
	cd server && go build -o ../bin/arcade ./cmd/server

up:
	docker compose up --build
