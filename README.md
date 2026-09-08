# Payment Gateway (Go)

Implementation of the CKO Payment Gateway take-home challenge. 

Please see [decision.md](decision.md) for the architecture and design rationale.

## Running the gateway

starts the bank simulator on localhost:8080

```bash
docker-compose up -d
```

starts the payment gateway on localhost:8090
```bash
go run main.go
```

once the gateway is up, `GET http://localhost:8090/ping` should return `{"message":"pong"}`.

The bank simulator's base URL can be overridden with the
`BANK_SIMULATOR_URL` environment variable. The defaults value is `http://localhost:8080`, which
matches `docker-compose.yml`.

## Running tests

```bash
go test ./...
```

All tests are self-contained, they use a mock acquiring bank
(`acquirer.MockAcquirer`), so `docker-compose up` is **not** required for
`go test ./...` to pass. 

The bank simulator is only needed to run the payment gateway end-to-end.

## API

Swagger UI is accessible at http://localhost:8090/swagger/index.html once the gateway is running.

### `POST /api/payments`

Every `POST` requires an `Idempotency-Key` header (any client-generated
unique string, e.g. a UUID). Retrying the exact same request with the same
key returns the original result instead of charging the bank again; reusing
a key with a different body is rejected with `409`.

1) Odd-ending card number -> the bank authorizes it

```bash
curl -s -X POST http://localhost:8090/api/payments \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: 3f8e6c2a-1b1f-4e2a-9f34-7a1c2e4b9d10" \
  -d '{
			"card_number": "2222405343248877",
			"expiry_month": 4,
			"expiry_year": 2030,
			"currency": "GBP",
			"amount": 100,
			"cvv": "123"
		}'
```

Response:

Response code: `201`
Response body:
```bash
{
    "id": "3f09cb33-2539-4ffd-b41e-c9251cf87110",
    "status": "Authorized",
    "card_number_last_four": "8877",
    "expiry_month": 4,
    "expiry_year": 2030,
    "currency": "GBP",
    "amount": 100
}
```

2) Even-ending card number -> the bank declines it
```bash
curl -s -X POST http://localhost:8090/api/payments \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: 9c1a5e77-4d3b-4f2e-8a90-2b6f7d1c5e33" \
  -d '{
			"card_number": "2222405343248878",
			"expiry_month": 4,
			"expiry_year": 2030,
			"currency": "USD",
			"amount": 250,
			"cvv": "456"
		}'
```
Response:

Response Code: `201`
Response body:
```bash
{
    "id": "ec444d51-5715-476f-b7f9-637cf1c00555",
    "status": "Declined",
    "card_number_last_four": "8878",
    "expiry_month": 4,
    "expiry_year": 2030,
    "currency": "USD",
    "amount": 250
}
```

3) Zero-ending card number -> the bank simulator is unreachable (503, after retries)
```bash
curl -s -X POST http://localhost:8090/api/payments \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: 7d2f4a91-6e5c-4b8a-a1d3-9f0e2c8b4a67" \
  -d '{
			"card_number": "2222405343248870",
			"expiry_month": 4,
			"expiry_year": 2030,
			"currency": "EUR",
			"amount": 500,
			"cvv": "789"
		}'
```
Response:

Response Code: `503 Service Unavailable`

Before returning this, the gateway retries the bank call up to 3 times
total with jittered backoff - a `503` here means the bank
was unreachable across all attempts, not just once.

A `503` is never cached against its `Idempotency-Key` - retrying the same
key (and body) once the bank recovers tries the bank again rather than
replaying the failure.

4) Invalid request -> rejected without ever calling the bank
```bash
curl -s -X POST http://localhost:8090/api/payments \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: a4b8e2c6-3d7f-4a91-b5e0-8c2f6a9d1e44" \
  -d '{
			"card_number": "12345",
			"expiry_month": 4,
			"expiry_year": 2030,
			"currency": "JPY",
			"amount": 100,
			"cvv": "123"
		}'
```

Response:

Response code: `400` - Bad Request
Response body:
```bash
{
    "status": "Rejected",
    "errors": [
        "card_number must be between 14 and 19 characters long",
        "currency must be one of: GBP, USD, EUR (got \"JPY\")"
    ]
}
```

Note: a `Rejected` payment request is never reached to bank, so its `id` can never be looked up here.

5) Missing `Idempotency-Key` header -> `400`
```bash
{
    "status": "Rejected",
    "errors": ["Idempotency-Key header is required"]
}
```

6) Same `Idempotency-Key` reused with a different request body -> `409`
```bash
{
    "status": "Rejected",
    "errors": ["Idempotency-Key was already used with a different request body"]
}
```

### `GET /api/payments/{id}`

7) 200 OK with the stored payment (masked card number, no CVV)
```bash
curl -s http://localhost:8090/api/payments/<id-from-a-201-response>

```
Response:
Response code: `200`
```bash
{
    "id": "04ded4d2-2581-437a-b421-a348ec35af63",
    "status": "Authorized",
    "card_number_last_four": "8877",
    "expiry_month": 4,
    "expiry_year": 2030,
    "currency": "GBP",
    "amount": 100
}
```

8) Payment ID does not exists - 404 Not Found

```bash
curl -s http://localhost:8090/api/payments/does-not-exist 
```

Response:

Response Code: `404 Not Found`

---

### Swagger
This template uses Swaggo to autodocument the API and create a Swagger spec. The Swagger UI is available at http://localhost:8090/swagger/index.html.