# Ganji — Full LNbits Cycle Testing Makefile
#
# Prerequisites:
#   - Go 1.22+
#   - PostgreSQL 14+ (or docker)
#   - LNbits instance (demo.lnbits.com for quick test, or self-hosted regtest)
#   - ngrok (for webhook testing locally)
#   - curl / httpie for API calls
#
# Quick start:
#   make setup          # Create .env from template
#   make db-up          # Start PostgreSQL via docker
#   make test-cycle     # Run full cycle test (requires configured .env)

# ─── Variables ──────────────────────────────────────────────────────────────
BACKEND_DIR := backend
FRONTEND_DIR := frontend
ENV_FILE := $(BACKEND_DIR)/.env
ENV_EXAMPLE := $(BACKEND_DIR)/.env.example
NGROK_URL ?= http://localhost:4040/api/tunnels
PORT := 8080

# ─── Setup ──────────────────────────────────────────────────────────────────

.PHONY: setup
setup: $(ENV_FILE) ## Create .env from .env.example
	@echo "✓ .env ready at $(ENV_FILE)"

$(ENV_FILE): $(ENV_EXAMPLE)
	@cp $(ENV_EXAMPLE) $(ENV_FILE)
	@echo "Created $(ENV_FILE) — edit it with your LNbits keys and webhook URL"

.PHONY: check-env
check-env: ## Verify required env vars are set
	@source $(ENV_FILE) 2>/dev/null || true; \
	for var in DATABASE_URL JWT_SECRET JWT_REFRESH_SECRET LNBITS_URL LNBITS_API_KEY LNBITS_ADMIN_KEY WEBHOOK_URL; do \
		val=$$var; \
		if [ -z "$${!val}" ] || [ "$${!val}" = "change-me-*" ]; then \
			echo "✗ Missing or default: $$var"; exit 1; \
		else \
			echo "✓ $$var is set"; \
		fi; \
	done

# ─── Database (docker-compose) ──────────────────────────────────────────────

.PHONY: db-up
db-up: ## Start PostgreSQL via docker-compose
	docker-compose up -d db
	@echo "Waiting for PostgreSQL..."
	@until docker-compose exec -T db pg_isready -U postgres -d ganji >/dev/null 2>&1; do sleep 1; done
	@echo "✓ PostgreSQL ready at localhost:5432"

.PHONY: db-down
db-down: ## Stop and remove PostgreSQL container
	docker-compose down

.PHONY: db-reset
db-reset: ## Reset database (destroy data)
	docker-compose down -v
	docker-compose up -d db
	@until docker-compose exec -T db pg_isready -U postgres -d ganji >/dev/null 2>&1; do sleep 1; done
	@echo "Database reset complete"

.PHONY: db-shell
db-shell: ## Open psql shell
	docker-compose exec db psql -U postgres -d ganji

# ─── Backend ────────────────────────────────────────────────────────────────

.PHONY: backend-deps
backend-deps: ## Download Go dependencies
	cd $(BACKEND_DIR) && go mod download

.PHONY: backend-build
backend-build: backend-deps ## Build backend
	cd $(BACKEND_DIR) && go build -o bin/ganji ./cmd/api

.PHONY: backend-test
backend-test: ## Run all backend tests
	cd $(BACKEND_DIR) && go test -v ./...

.PHONY: backend-test-deals
backend-test-deals: ## Run deal service tests (LNbits cycle)
	cd $(BACKEND_DIR) && go test -v ./internal/deals/...

.PHONY: backend-lint
backend-lint: ## Run linter
	cd $(BACKEND_DIR) && go vet ./... && gofmt -l .

.PHONY: run
run: check-env backend-build ## Run backend server
	cd $(BACKEND_DIR) && ./bin/ganji

.PHONY: run-dev
run-dev: check-env ## Run backend with live reload (requires air)
	cd $(BACKEND_DIR) && air -c .air.toml

# ─── LNbits Helpers ─────────────────────────────────────────────────────────

.PHONY: lnbits-health
lnbits-health: ## Test LNbits connectivity
	@source $(ENV_FILE) 2>/dev/null || true; \
	curl -s -H "X-Api-Key: $$LNBITS_API_KEY" $$LNBITS_URL/api/v1/wallet | jq .

.PHONY: lnbits-fund-regtest
lnbits-fund-regtest: ## Fund LNbits wallet on regtest (requires bitcoind container)
	@echo "Generating 101 blocks to fund wallet..."
	docker exec bitcoind bitcoin-cli -regtest -rpcuser=user -rpcpassword=pass generatetoaddress 101 $$(docker exec bitcoind bitcoin-cli -regtest -rpcuser=user -rpcpassword=pass getnewaddress)
	@echo "✓ Wallet funded"

# ─── ngrok ──────────────────────────────────────────────────────────────────

.PHONY: ngrok
ngrok: ## Start ngrok tunnel to localhost:8080
	@which ngrok >/dev/null || (echo "ngrok not installed"; exit 1)
	ngrok http $(PORT)

.PHONY: ngrok-url
ngrok-url: ## Get ngrok public URL
	@curl -s $(NGROK_URL) | jq -r '.tunnels[0].public_url'

.PHONY: update-webhook-url
update-webhook-url: ## Update WEBHOOK_URL in .env with current ngrok URL
	@URL=$$(curl -s $(NGROK_URL) | jq -r '.tunnels[0].public_url'); \
	if [ "$$URL" = "null" ] || [ -z "$$URL" ]; then \
		echo "No ngrok tunnel found. Run 'make ngrok' first."; exit 1; \
	fi; \
	sed -i "s|^WEBHOOK_URL=.*|WEBHOOK_URL=$${URL}/webhooks/lnbits|" $(ENV_FILE); \
	echo "Updated WEBHOOK_URL to $$URL/webhooks/lnbits"

# ─── Full Cycle Testing ─────────────────────────────────────────────────────

.PHONY: test-cycle
test-cycle: check-env backend-build ## Run full LNbits cycle test (requires running backend)
	@echo "=== GANJI FULL LNbits CYCLE TEST ==="
	@echo ""
	@echo "Prerequisites:"
	@echo "  1. Backend running: 'make run' in another terminal"
	@echo "  2. ngrok running: 'make ngrok' in another terminal"
	@echo "  3. WEBHOOK_URL updated: 'make update-webhook-url'"
	@echo "  4. LNbits webhook configured with ngrok URL + secret"
	@echo ""
	@read -p "Press Enter when ready..."; \
	$(MAKE) _test-cycle-steps

# Internal: actual test steps
.PHONY: _test-cycle-steps
_test-cycle-steps:
	@source $(ENV_FILE) 2>/dev/null || true; \
	BASE=http://localhost:$(PORT); \
	echo ""; \
	echo "▶ Step 1: Register freelancer"; \
	FREELANCER_RESP=$$(curl -s -X POST $$BASE/auth/register \
		-H "Content-Type: application/json" \
		-d '{"email":"freelancer@test.com","password":"password123","display_name":"Test Freelancer"}'); \
	echo $$FREELANCER_RESP | jq .; \
	FREELANCER_TOKEN=$$(echo $$FREELANCER_RESP | jq -r '.access_token'); \
	[ "$$FREELANCER_TOKEN" != "null" ] || (echo "✗ Registration failed"; exit 1); \
	echo "✓ Freelancer token obtained"; \
	echo ""; \
	echo "▶ Step 2: Register client"; \
	CLIENT_RESP=$$(curl -s -X POST $$BASE/auth/register \
		-H "Content-Type: application/json" \
		-d '{"email":"client@test.com","password":"password123","display_name":"Test Client"}'); \
	echo $$CLIENT_RESP | jq .; \
	CLIENT_TOKEN=$$(echo $$CLIENT_RESP | jq -r '.access_token'); \
	[ "$$CLIENT_TOKEN" != "null" ] || (echo "✗ Client registration failed"; exit 1); \
	echo "✓ Client token obtained"; \
	echo ""; \
	echo "▶ Step 3: Create deal (need a real bolt11 payee invoice)"; \
	echo "   Generate a Lightning invoice from your wallet (Phoenix, Breez, etc.)"; \
	echo "   Example: lnbc50000u1p3xyz..."; \
	read -p "   Enter payee_invoice (bolt11): " PAYEE_INVOICE; \
	[ -n "$$PAYEE_INVOICE" ] || (echo "✗ No invoice provided"; exit 1); \
	DEAL_RESP=$$(curl -s -X POST $$BASE/deals \
		-H "Authorization: Bearer $$FREELANCER_TOKEN" \
		-H "Content-Type: application/json" \
		-d "$$(jq -n --arg t "Test Deal" --argjson a 50000 --arg s "test" --arg c "client@test.com" --arg p "$$PAYEE_INVOICE" \
			'{title: $$t, amount_sats: $$a, source_platform: $$s, client_email: $$c, payee_invoice: $$p}')"); \
	echo $$DEAL_RESP | jq .; \
	DEAL_ID=$$(echo $$DEAL_RESP | jq -r '.deal.id'); \
	SHARE_TOKEN=$$(echo $$DEAL_RESP | jq -r '.deal.share_token'); \
	BOLT11=$$(echo $$DEAL_RESP | jq -r '.deal.bolt11'); \
	[ "$$DEAL_ID" != "null" ] || (echo "✗ Deal creation failed"; exit 1); \
	echo "✓ Deal created: $$DEAL_ID"; \
	echo "✓ Share token: $$SHARE_TOKEN"; \
	echo "✓ Hold invoice (bolt11): $$BOLT11"; \
	echo ""; \
	echo "▶ Step 4: Public deal link"; \
	echo "   Open: $$BASE/public/deals/$$SHARE_TOKEN"; \
	echo ""; \
	echo "▶ Step 5: Pay the hold invoice"; \
	echo "   Scan this bolt11 with your Lightning wallet and PAY:"; \
	echo "   $$BOLT11"; \
	echo ""; \
	read -p "   Press Enter after payment is confirmed..."; \
	echo ""; \
	echo "▶ Step 6: Check payment status (poll)"; \
	curl -s -X GET $$BASE/deals/$$DEAL_ID/payment \
		-H "Authorization: Bearer $$FREELANCER_TOKEN" | jq .; \
	echo ""; \
	echo "▶ Step 7: Freelancer submits work"; \
	curl -s -X POST $$BASE/deals/$$DEAL_ID/submit \
		-H "Authorization: Bearer $$FREELANCER_TOKEN" | jq .; \
	echo ""; \
	echo "▶ Step 8: Client approves (releases escrow)"; \
	curl -s -X POST $$BASE/deals/$$DEAL_ID/approve \
		-H "Authorization: Bearer $$CLIENT_TOKEN" | jq .; \
	echo ""; \
	echo "▶ Step 9: Verify final deal state"; \
	curl -s -X GET $$BASE/deals/$$DEAL_ID \
		-H "Authorization: Bearer $$FREELANCER_TOKEN" | jq .; \
	echo ""; \
	echo "=== CYCLE COMPLETE ==="; \
	echo ""; \
	echo "Deal should now be 'released' — funds settled from hold and paid to freelancer."; \
	echo "Check CV: $$BASE/cv/<freelancer-slug> (after deal released, CV entry auto-anchored)"

# Quick automated test (no manual payment)
.PHONY: test-cycle-auto
test-cycle-auto: check-env backend-test-deals ## Run automated unit tests for LNbits cycle
	@echo "Running automated deal service tests..."
	cd $(BACKEND_DIR) && go test -v -run "TestCreateDeal|TestSubmitWork|TestApproveDeal|TestDisputeDeal|TestResolveDispute|TestSweep|TestPayeeInvoiceRotation|TestCheckPayment" ./internal/deals/...

# ─── Frontend ───────────────────────────────────────────────────────────────

.PHONY: frontend-deps
frontend-deps: ## Install frontend dependencies
	cd $(FRONTEND_DIR) && npm install

.PHONY: frontend-build
frontend-build: frontend-deps ## Build frontend
	cd $(FRONTEND_DIR) && npm run build

.PHONY: frontend-dev
frontend-dev: frontend-deps ## Run frontend dev server
	cd $(FRONTEND_DIR) && npm run dev

# ─── Full Stack (docker-compose) ────────────────────────────────────────────

.PHONY: up
up: ## Start full stack (db + api + frontend) via docker-compose
	docker-compose up -d --build

.PHONY: up-logs
up-logs: ## Start full stack and follow logs
	docker-compose up --build

.PHONY: down
down: ## Stop full stack
	docker-compose down

.PHONY: logs
logs: ## Follow all service logs
	docker-compose logs -f

.PHONY: logs-api
logs-api: ## Follow API logs
	docker-compose logs -f api

.PHONY: dev
dev: db-up ## Start dev stack (db only), run backend/frontend locally
	@echo "Starting dev stack..."
	@echo "Run 'make run' in one terminal, 'make frontend-dev' in another"

.PHONY: clean
clean: ## Clean build artifacts
	rm -rf $(BACKEND_DIR)/bin
	rm -rf $(FRONTEND_DIR)/.next
	rm -rf $(FRONTEND_DIR)/node_modules
	docker-compose down -v --remove-orphans 2>/dev/null || true

# ─── Help ───────────────────────────────────────────────────────────────────

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-25s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help