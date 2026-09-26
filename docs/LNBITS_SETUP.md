# LNbits Setup for Ganji

Ganji uses **LNbits** as the Lightning wallet backend. You need a wallet with two API keys:

| Key | Purpose | Ganji Config |
|---|---|---|
| **Invoice Key** | Creates hold invoices (read-only) | `LNBITS_API_KEY` |
| **Admin Key** | Settles, cancels, pays out (money moves) | `LNBITS_ADMIN_KEY` |

> ⚠️ **The admin key is required.** The invoice key alone cannot settle/cancel holds or pay out.

---

## Option 1: LNbits Demo (Quickest for Testing)

1. Go to https://demo.lnbits.com
2. Create a wallet (any name)
3. **Wallet → Settings → API Keys**:
   - Copy **Invoice key** → `LNBITS_API_KEY`
   - Copy **Admin key** → `LNBITS_ADMIN_KEY`
4. Set in `.env`:
   ```env
   LNBITS_URL=https://demo.lnbits.com
   LNBITS_API_KEY=your_invoice_key
   LNBITS_ADMIN_KEY=your_admin_key
   WEBHOOK_URL=http://your-ngrok-url/webhooks/lnbits  # or your public URL
   ```

**Caveats:**
- Demo is on testnet/mainnet — real sats move
- Webhooks need a public URL (use ngrok: `ngrok http 8080`)
- Shared instance — don't put real funds

---

## Option 2: Self-Hosted LNbits + Bitcoin Core Regtest (Recommended for Dev)

### Prerequisites
- Docker
- Bitcoin Core regtest (or use LNbits' built-in funding source)

### 1. Start Bitcoin Core Regtest
```bash
# Option A: Use the bundled bitcoind in LNbits docker-compose
# (see LNbits repo for full compose file)

# Option B: Quick standalone regtest
docker run -d --name bitcoind \
  -v bitcoind-data:/bitcoin \
  -p 18443:18443 -p 18444:18444 \
  ruimarinho/bitcoin-core:26.0 \
  -regtest -fallbackfee=0.0001 -server -rpcuser=user -rpcpassword=pass
```

### 2. Start LNbits (Docker Compose)
```yaml
# docker-compose.yml
version: '3.8'
services:
  lnbits:
    image: lnbits/lnbits:latest
    ports:
      - "5000:5000"
    environment:
      - LNBITS_NETWORK=regtest
      - LNBITS_BACKEND_WALLET_CLASS=LndRestWallet
      - LND_REST_ENDPOINT=http://lnd:8080
      - LND_REST_CERT=/lnd/tls.cert
      - LND_REST_MACAROON=/lnd/admin.macaroon
    volumes:
      - lnbits-data:/data
    depends_on:
      - lnd

  lnd:
    image: lightninglabs/lnd:v0.18.0-beta
    ports:
      - "10009:10009"
      - "8080:8080"
    volumes:
      - lnd-data:/lnd
      - ./lnd.conf:/etc/lnd/lnd.conf
    depends_on:
      - bitcoind

  bitcoind:
    image: ruimarinho/bitcoin-core:26.0
    ports:
      - "18443:18443"
    volumes:
      - bitcoind-data:/bitcoin
    command: >
      -regtest -fallbackfee=0.0001 -server
      -rpcuser=user -rpcpassword=pass
      -rpcallowip=0.0.0.0/0

volumes:
  lnbits-data:
  lnd-data:
  bitcoind-data:
```

**Simpler alternative:** Use [LNbits' official docker-compose](https://github.com/lnbits/lnbits/blob/main/docker-compose.yml) which includes LND + bitcoind pre-wired.

### 3. Get Keys
1. Open LNbits at `http://localhost:5000`
2. Create wallet
3. Settings → API Keys → copy both keys
4. Set in `.env`:
   ```env
   LNBITS_URL=http://localhost:5000
   LNBITS_API_KEY=your_invoice_key
   LNBITS_ADMIN_KEY=your_admin_key
   WEBHOOK_URL=http://localhost:8080/webhooks/lnbits
   ```

### 4. Fund the Wallet (Regtest)
```bash
# Generate blocks to fund LNbits wallet
docker exec bitcoind bitcoin-cli -regtest -rpcuser=user -rpcpassword=pass generatetoaddress 101 $(docker exec bitcoind bitcoin-cli -regtest -rpcuser=user -rpcpassword=pass getnewaddress)
```

---

## Option 3: Your Own LNbits Instance

If you already run LNbits (VPS, Umbrel, Start9, etc.):
1. Create dedicated wallet for Ganji
2. Copy Invoice Key + Admin Key
3. Set `LNBITS_URL` to your instance
4. Ensure webhook URL is publicly accessible

---

## Webhook Configuration

Ganji receives payment notifications via LNbits webhooks.

### Required
```env
WEBHOOK_URL=https://your-domain.com/webhooks/lnbits
LNBITS_WEBHOOK_SECRET=your-webhook-secret  # from LNbits wallet settings
```

### Local Development with ngrok
```bash
# Terminal 1: Start backend
cd backend && go run ./cmd/api

# Terminal 2: Expose port 8080
ngrok http 8080
# Copy the https URL (e.g., https://abc123.ngrok.io)
# Set WEBHOOK_URL=https://abc123.ngrok.io/webhooks/lnbits
```

### In LNbits Wallet Settings
1. Extensions → **Webhooks** → Enable
2. Add webhook:
   - URL: `https://your-ngrok-url/webhooks/lnbits`
   - Secret: generate random string → put in `LNBITS_WEBHOOK_SECRET`

---

## Payee Invoice (Freelancer Payout)

When creating a deal, the freelancer provides their **own Lightning invoice** (`payee_invoice`). This is where Ganji pays out after client approves.

**For testing:** Use any Lightning wallet (Phoenix, Breez, Wallet of Satoshi, Zap, BlueWallet) to generate a receive invoice.

```bash
# Example payee_invoice format
lnbc50000u1p3xyz...  # bolt11 invoice from freelancer's wallet
```

---

## Complete `.env` Example

```env
# Database
DATABASE_URL=postgres://postgres:postgres@localhost:5432/ganji?sslmode=disable

# Auth
JWT_SECRET=super-secret-access-key-change-in-production
JWT_REFRESH_SECRET=super-secret-refresh-key-change-in-production

# LNbits (using demo for example)
LNBITS_URL=https://demo.lnbits.com
LNBITS_API_KEY=your-invoice-key-from-lnbits
LNBITS_ADMIN_KEY=your-admin-key-from-lnbits
LNBITS_WEBHOOK_SECRET=random-webhook-secret
WEBHOOK_URL=https://abc123.ngrok.io/webhooks/lnbits

# Storage
STORAGE_PATH=./uploads
MAX_UPLOAD_BYTES=10485760

# Frontend
FRONTEND_URL=http://localhost:3000

# Email (optional)
SMTP_HOST=
SMTP_PORT=587
SMTP_USER=
SMTP_PASS=
SMTP_FROM=noreply@ganji.local
SMTP_ENCRYPTION=starttls

# OTS (optional)
OTS_ESPLORA_URL=https://blockstream.info/api
OTS_UPGRADE_INTERVAL_SECONDS=21600

# Operators (optional)
OPERATOR_EMAILS=admin@yourdomain.com
```

---

## Quick Test Flow

```bash
# 1. Start backend
cd backend && go run ./cmd/api

# 2. Signup freelancer
curl -X POST http://localhost:8080/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"freelancer@test.com","password":"password123","display_name":"Test Freelancer"}'

# 3. Login → get access_token
curl -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"freelancer@test.com","password":"password123"}'

# 4. Create deal (replace payee_invoice with real bolt11 from your wallet)
curl -X POST http://localhost:8080/deals \
  -H "Authorization: Bearer <access_token>" \
  -H "Content-Type: application/json" \
  -d '{"title":"Test Deal","amount_sats":50000,"source_platform":"telegram","client_email":"client@test.com","payee_invoice":"lnbc50000u1p3xyz..."}'

# 5. Get public link from response → open in browser
#    https://localhost:8080/public/deals/<share_token>

# 6. Scan bolt11 with Lightning wallet → PAY

# 7. Deal auto-locks → freelancer submits work → client approves
```

---

## Troubleshooting

| Issue | Fix |
|---|---|
| `LNBITS_ADMIN_KEY` invalid | Must be **admin key**, not invoice key |
| Webhook not received | Check `WEBHOOK_URL` is public HTTPS; check `LNBITS_WEBHOOK_SECRET` matches |
| Hold invoice not created | Verify `LNBITS_API_KEY` and `LNBITS_URL` |
| Payout fails | Admin key needs wallet balance; fund LNbits wallet first |
| `context deadline exceeded` | Increase `LNBITS_HOLD_INVOICE_EXPIRY_SECONDS` or check LNbits connectivity |

---

## Security Notes

- **Never commit `.env`** — it's in `.gitignore`
- **Admin key = full wallet control** — treat like a password
- **Use dedicated wallet** for Ganji, not your personal wallet
- **Regtest for development** — no real funds at risk
- **Rotate keys** if exposed