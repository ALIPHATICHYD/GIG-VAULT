.PHONY: api web seed test tunnel

api:            ## run the Go API + worker (reads backend/.env)
	cd backend && go run ./cmd/api

web:            ## run the React app on :5173
	cd web && npm install && npm run dev

seed:           ## WIPES the database, then builds a ready-to-demo state
	cd backend && go run ./cmd/seed -reset

test:           ## vet + tests. Set TEST_DATABASE_URL to include the database-backed ones
	cd backend && go vet ./... && go test ./... -timeout 40m

tunnel:         ## public HTTPS URL for Busha webhooks
	cloudflared tunnel --url http://localhost:8080
