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

1) Odd-ending card number -> the bank authorizes it

```bash
curl -s -X POST http://localhost:8090/api/payments \
  -H "Content-Type: application/json" \
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

3) Zero-ending card number -> the bank simulator is unreachable (503)
```bash
curl -s -X POST http://localhost:8090/api/payments \
  -H "Content-Type: application/json" \
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

4) Invalid request -> rejected without ever calling the bank
```bash
curl -s -X POST http://localhost:8090/api/payments \
  -H "Content-Type: application/json" \
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

Note: a `Rejected` payment request is never reached to bank, so its `id` can never be looked up here - see
decision.md D2.

### `GET /api/payments/{id}`

5) 200 OK with the stored payment (masked card number, no CVV)
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

6) Payment ID does not exists - 404 Not Found

```bash
curl -s http://localhost:8090/api/payments/does-not-exist 
```

Response:

Response Code: `404 Not Found`

---

### Swagger
This template uses Swaggo to autodocument the API and create a Swagger spec. The Swagger UI is available at http://localhost:8090/swagger/index.html.