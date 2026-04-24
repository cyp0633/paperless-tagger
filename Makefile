.PHONY: dev build frontend backend clean

# Build everything (frontend first, then Go binary)
build: frontend
	go build -o paperless-tagger .

# Build frontend
frontend:
	cd web-app && bun install && bun run build

# Run in development (starts Go backend; run `make frontend-dev` in another terminal)
backend:
	go run .

# Run Vite dev server (proxies /api to :8080)
frontend-dev:
	cd web-app && bun install && bun run dev

# Full dev: build frontend + run Go (for quick iteration without hot-reload)
dev: frontend backend

clean:
	rm -f paperless-tagger
	rm -rf web-app/dist
