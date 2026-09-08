# Payment Gateway - Architecture Decisions

Notes on how I approached this and why. Not every trivial choice is written
down here, just the ones I expect to have to defend or that someone picking
up this code later would wonder about.

## 1. Scope recap

In scope:
- `POST /api/payments` - process a card payment, returning `Authorized`,
  `Declined`, or `Rejected`.
- `GET /api/payments/{id}` - retrieve a previously made payment.
- Field validation for the payment request (card number, expiry, currency,
  amount, cvv).
- Integration with the provided Mountebank bank simulator (`docker-compose up`,
  `POST http://localhost:8080/payments`).
- Automated tests.

Left out on purpose: a real database (the in-memory repo is enough here), a
real acquiring bank (the simulator covers it), and anything like merchant
auth or rate limiting - none of that is asked fo. Idempotency keys and bounded
retry with backoff on the bank call were added later.

## 2. Data type change for CardNumberLastFour and Cvv

`internal/models/payment.go` had `CardNumberLastFour` and `Cvv` typed as
`int`. That's a real bug, not a style nit - `int` silently eats a leading
zero, so a CVV of `012` turns into `12`. Both need to be strings.

## 3. Architecture overview

Layered, still a single binary, no new services:

```
HTTP (chi router)
  -> handlers    (decode/encode JSON, map domain errors -> HTTP status)
      -> service  (orchestrates: validate -> call bank -> persist -> respond)
          -> validator        (pure functions, field-level rules -> Rejected)
          -> acquirer client  (HTTP client to the bank simulator)
          -> repository       (in-memory store, provided test double)
```

The existing CKO code has the handler talking directly to the repository. I split out
a `service` package because `POST` actually has three jobs - validate, call
an external system, persist - and none of that is really an HTTP concern.
Pulling it out means I can test the whole payment flow without spinning up
`net/http`, and it keeps the handler a thin adapter rather than turning into
unnecessary layering for its own sake.

## 4. Key decisions

### D1 - Service layer between handler and repository/bank

`internal/service` owns `ProcessPayment` and `GetPayment`. `POST
/api/payments` has to coordinate validation, an external HTTP call and
storage, which is business logic, not routing - keeping it there lets me
unit-test the whole flow with a fake bank client and the real in-memory repo,
no HTTP server involved.

The alternative was leaving everything in the handler, the way the GET stub
already does. That's fine for a single lookup but falls apart for a 3-step
orchestration - the handler would end up untestable without a live server
and hard to follow.

### D2 - Rejected payments never call the bank, and are not persisted

Validation runs first. Any failure returns `Rejected` immediately - the bank
is never called, and nothing gets written to the repository. This is mentioned in the requirements.

### D3 - Currency allow-list of exactly 3 ISO codes

Validating against a fixed list - `GBP`, `USD`, `EUR` - rather than the full
ISO 4217 table, as mentioned in requirement.

### D4 - Never persist or log the full PAN or CVV

The domain/response models only ever hold the last four digits of the card
number. The full PAN and CVV exist only transiently, while validating and
making the outbound call to the bank simulator - never written to the
repository or logged.

### D5 - Payment IDs are UUIDv4

Using `google/uuid` to generate the `Id`, could've done it with
`crypto/rand` and zero new deps, but UUIDv4 is the unambiguous
choice, and the spec explicitly allows "whatever format... e.g. a GUID is
fine".

### D6 - Keep the in-memory repository, but make it concurrency-safe

Keeping the provided repository as instructed, no real DB - but backing it
with a `map[string]Payment` guarded by `sync.RWMutex` instead of the
original unsynchronized slice scan. `net/http` serves requests concurrently
by default, so the unlocked slice is a genuine data race (`go test -race`
flags it).

### D7 - Bank simulator client as a small interface

One small interface: `Acquirer.Authorize(ctx, BankRequest) (BankResponse,
error)`. The real version calls the bank simulator over HTTP
(`POST http://localhost:8080/payments`). A mock version stands in for tests.

This lets tests cover odd/even/zero-ending card numbers, timeouts, and 503s
without needing `docker-compose up`. I didn't add any other interfaces (like
a `Repository` interface) - nothing else here needs to be swapped or
mocked.


### D8 - How to handle a bank that can't be reached (503 / timeout)

If the bank simulator returns `503` or the call times out or fails at the
transport level, that's neither `Declined` (would misreport a real decline)
nor `Rejected` (the request was valid, we did attempt to call the bank). The
gateway returns a `5xx` to the merchant and does not persist a record, since
the actual outcome is unknown and there's nothing correct to show on a later
GET.

The three spec statuses are all outcomes the gateway is certain about. A
bank outage is a fourth case the spec just doesn't name, and inventing a
status for it felt worse than surfacing the failure honestly. Explicitly not
doing retries/backoff here - out of scope, not required, and it would add
complexity the task doesn't call for.

### D9 - HTTP status code mapping

| Outcome | HTTP status |
|---|---|
| Authorized / Declined (bank responded, payment stored) | `201 Created` |
| Rejected (validation failed, nothing stored) | `400 Bad Request` |
| Bank unreachable / simulator 503 / timeout | `503 Service Unavailable` |
| GET, payment found | `200 OK` |
| GET, payment not found | `404 Not Found` (fixes a bug in the original scaffold) |

### D10 - Idempotency-Key required on POST /api/payments

`POST /api/payments` requires an `Idempotency-Key` header. `internal/idempotency`
keeps a per-key lock plus a cache of completed responses, both in-process:

- A retried request (same key, same body) blocks behind any request already
  in flight for that key, then replays the exact same response once it's
  done - the bank is only ever charged once per key.
- The same key reused with a different body is rejected with `409`, rather
  than silently returning the wrong payment.
- A `Rejected` (400) or bank-unavailable (503) outcome is deliberately *not*
  cached - nothing was charged, so a retry with the same key should get a
  fresh attempt, not a replayed failure.

Why a header and not a body field: it's a transport/retry concern, not part
of the payment itself (this is also how Stripe and Checkout.com's own APIs
do it), so it doesn't belong on `PaymentRequest`.

Why required rather than optional: an optional key means most callers won't
bother, and a payment gateway silently double-charging the client who forgot
the header is worse than a slightly stricter API. It is a breaking change to
the existing contract, which is worth calling out explicitly.

Why lock per key instead of just checking-then-storing: two copies of the
same retried request can arrive genuinely concurrently (e.g. a client
timeout firing while the original call is still in flight). Without a lock,
both could see "no cached response yet" and both call the bank. The lock
serializes them so the second one waits for the first's result instead of
racing it.

### D11 - retry with backoff around the acquiring bank call

`internal/acquirer/retry.go` adds `RetryingAcquirer`, a decorator wrapping
any `Acquirer`, and it's what actually gets wired up in
`internal/api/api.go` now - `HTTPAcquirer` itself is unchanged and still
just makes one HTTP call. On `ErrBankUnavailable` it retries up to 3
attempts total, backing off 100ms/200ms with full jitter (capped at 1s), and
gives up if the caller's context is cancelled first.

What it deliberately does *not* retry:
- A decision the bank actually returned - `Authorized` or `Declined`. Those
  aren't failures, they're answers; retrying one would mean asking the bank
  the same question twice for no reason.
- Any error that isn't `ErrBankUnavailable` (e.g. the `json.Marshal`/
  `http.NewRequestWithContext` failures in `HTTPAcquirer`). Those are local
  bugs, not transient bank problems - retrying them just wastes time before
  failing the same way again.

Why a decorator instead of putting the loop inside `HTTPAcquirer`: keeps
`HTTPAcquirer` doing exactly one thing (one HTTP call, mapped to the
package's error contract), keeps the retry policy unit-testable against a
fake `Acquirer` with no HTTP involved (`retry_test.go`), and follows the
same "wrap the interface, don't grow the concrete type" shape as D7 already
established.

Why full jitter over fixed exponential backoff: with fixed delays, every
replica retrying a struggling bank at once re-synchronizes into further
simultaneous bursts (thundering herd) - full jitter (a random delay in
`[0, backoff]` rather than exactly `backoff`) spreads retries out instead.

**Interaction with D10:** the idempotency lock for a key is held for the
entire `ProcessPayment` call, so all the retry's internal attempts happen as
one logical attempt from the idempotency store's point of view - a
concurrent duplicate request still just waits for the whole thing (retries
included) to finish, rather than racing in partway through.

**Known limitation, stated plainly:** a retry assumes the failed attempt
never reached the bank's actual authorization logic - true for a connection
refusal or our own timeout, but not guaranteed for a slow response that
timed out after the bank had already decided. A real acquirer connection
would need its own request-level idempotency key (most real ones - card
networks included - support this) so a retried authorization can't become a
second charge; the Mountebank simulator here has no such mechanism, so this
is a known gap rather than a solved one. No circuit breaker either - out of
scope for now, and 3 bounded attempts with a 5s per-attempt timeout already
caps how much damage one struggling bank call can do to a single request.

## 5. Future considerations: reliability & scalability (out of scope here)

This exercise deliberately left out a real database (see §1, D11) to avoid
over-engineering a take-home. Idempotency keys (D10) and bank-call retry
(D11) are handled, but both only within a single process - taking this from
an exercise to a production gateway would mean revisiting the rest of these
tradeoffs.

### Reliability

- **No circuit breaker.** We retry a failing bank 3 times (D11), but every
  replica does this on its own. Under heavy load that's 3x the traffic
  hitting a bank that's already struggling. A circuit breaker would stop
  calling out for a bit after enough failures, instead of piling on.
- **The idempotency store (D10) only lives in one process.** It's just an
  in-memory map. A restart wipes it, and two replicas behind a load balancer
  won't share it - so a retry that lands on a different replica could still
  double-charge, which is exactly what D10 is supposed to prevent. Fixing
  this needs the same shared store as the repository (below), plus a TTL so
  old keys get cleaned up (Stripe uses 24h).

### Scalability

- **The in-memory repository is the main blocker to running more than one
  replica** (the idempotency store has the same issue). It's just a map in
  memory, so each replica sees a different set of payments - a `GET` could
  `404` on one replica right after a `POST` succeeded on another. Fixing
  this means moving to a real shared store (Postgres, or Redis if that's
  consistent enough), the same way `Acquirer` is already injected (D7).
- **Everything else already scales fine.** Handlers don't hold state, the
  acquirer client is just a plain `http.Client`, and the bank call has a
  timeout so it can't hang forever. Once the repository moves out of
  memory, adding more replicas is just infrastructure, not code changes.
- **At real scale, the bank itself is the bottleneck**, not this gateway -
  worth tuning connection pooling on the `http.Client`, and, as above, a
  circuit breaker so replicas back off instead of hammering a struggling
  bank.
