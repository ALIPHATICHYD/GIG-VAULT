.PHONY: api web test tunnel

api:            ## run the Go API + worker (reads backend/.env)
	cd backend && go run ./cmd/api

web:            ## run the React app on :5173
	cd web && npm install && npm run dev

test:
	cd backend && go vet ./... && go test ./...

tunnel:         ## public HTTPS URL for Busha webhooks
	cloudflared tunnel --url http://localhost:8080
