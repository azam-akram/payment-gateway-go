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
auth or rate limiting - none of that is asked for, and bolting it on would
just be over-engineering a take-home. Idempotency keys (D12) were added
later, since a payment gateway that can double-charge on a client retry is a
correctness gap worth closing even outside the original brief.

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

The scaffold has the handler talking directly to the repository. I split out
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
is never called, and nothing gets written to the repository. This is spelled
out in the requirements ("Rejected - no payment could be created as invalid
information was supplied ... without calling the acquiring bank"), and it
also sidesteps having to invent a storage/lookup story for records that have
no useful fields (no last-four, no confirmed-valid expiry, etc.).

One consequence worth calling out: `GET /api/payments/{id}` can never return
`Rejected` - only `Authorized`/`Declined` are retrievable. I've said this
explicitly in the README so it doesn't read as an oversight.

### D3 - Currency allow-list of exactly 3 ISO codes

Validating against a fixed list - `GBP`, `USD`, `EUR` - rather than the full
ISO 4217 table. The brief says "validates against no more than 3 currency
codes," so a full table would be more than asked for and hard to justify
testing exhaustively anyway.

### D4 - Never persist or log the full PAN or CVV

The domain/response models only ever hold the last four digits of the card
number. The full PAN and CVV exist only transiently, while validating and
making the outbound call to the bank simulator - never written to the
repository or logged. This is directly required by the spec ("it is fine to
return the last four digits"), and it's also just the realistic compliance
posture for anything touching card data (PCI-DSS style data minimization),
even in a take-home.

### D5 - Payment IDs are UUIDv4

Using `google/uuid` to generate the `Id` (could've done it with
`crypto/rand` and zero new deps, but UUIDv4 is the boring, unambiguous
choice, and the spec explicitly allows "whatever format... e.g. a GUID is
fine"). It also sorts as an opaque identifier - a merchant can't infer
volume or sequence from it.

### D6 - Keep the in-memory repository, but make it concurrency-safe

Keeping the provided repository as instructed, no real DB - but backing it
with a `map[string]Payment` guarded by `sync.RWMutex` instead of the
original unsynchronized slice scan. `net/http` serves requests concurrently
by default, so the unlocked slice is a genuine data race (`go test -race`
flags it). This is a small, justifiable fix, not scope creep - it just makes
the existing test double safe under the concurrency the server already
allows.

### D7 - Bank simulator client as a small interface

One narrow interface - `Acquirer.Authorize(ctx, BankRequest) (BankResponse,
error)` - with an HTTP implementation that calls
`POST http://localhost:8080/payments`, plus a fake for unit tests. This is
the one seam that touches a network dependency, so it's the one place an
interface earns its keep: it lets the service layer be tested
deterministically (odd/even/zero-ending card numbers, timeouts, 503s)
without needing `docker-compose up` for every `go test` run. I didn't add
interfaces anywhere else - no `Repository` interface, for instance - because
nothing else here needs to be swapped or mocked.

Base URL is configurable via an env var, falling back to the simulator's
default (`http://localhost:8080`), so the demo runs against the real
simulator with zero flags.

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

`201` reflects that a payment *resource* got created for both Authorized and
Declined - both are stored and retrievable, a decline is a legitimate
outcome, not an API failure. `400` fits a rejected request that created
nothing. `404` is just the conventional code for a missing resource, and
matches what the given test already asserts.

### D10 - Testing strategy

- Table-driven unit tests for the validator, one row per field rule from the
  requirements.
- Service-level tests against the real in-memory repository plus a fake
  `Acquirer`, covering odd/even/zero-ending card numbers and simulated
  503s/timeouts.
- Handler tests via `httptest` - fixed the existing GET test's status-code
  bug and added POST coverage for the Authorized/Declined/Rejected paths.
- One documented manual/curl check against the real Mountebank simulator
  (`docker-compose up`), to actually prove the integration works
  end-to-end. Deliberately not part of `go test ./...`, so CI and local runs
  stay fast and don't need Docker.

Matches "your choice which type of tests" while keeping the bank dependency
out of the default test run.

### D11 - Keep it simple: no new frameworks, no DI container, no repository interface

Sticking with `chi` (already in `go.mod`), constructor injection (as the
scaffold already does for the repository), and concrete types everywhere
except the one `Acquirer` interface from D7. The brief warns against
over-engineering directly, and every abstraction here has to earn its place
by making something genuinely testable or swappable - nothing else
qualified.

### D12 - Idempotency-Key required on POST /api/payments

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

**Known limitation:** the lock/cache map lives in one process, same
limitation as the repository (D6) - see §7. It also never evicts, unlike
real idempotency stores (Stripe expires keys after 24h); fine for a
single-process demo, not for production.

## 5. Assumptions

- `expiry_date` sent to the bank simulator is formatted `MM/YYYY`, built
  from the request's `expiry_month`/`expiry_year`.
- "Expiry year must be in the future" means the last day of
  `expiry_month/expiry_year` hasn't passed yet relative to now (month+year
  combined, per the requirements note) - not "year alone in the future."
- Amount is an integer in minor units as given. No currency-specific
  decimal-places handling (e.g. JPY having 0 minor units) - the fixed
  3-currency list in D3 sidesteps that, or it's a known simplification if
  not.
- `card_number`/`cvv` are strings on the wire, to preserve leading zeros and
  match validation rules that are about length/content rather than numeric
  value. This differs from the scaffold's original request model and was
  corrected.
- No authentication on the API - not requested, and out of scope for what
  this exercise is testing.

## 6. Open questions for the demo

Things I'd rather flag than silently decide on my own:

- The exact 3-currency list (`GBP`/`USD`/`EUR`) is my pick, not spec-
  mandated - happy to swap it for whatever's expected.
- Whether `201 Created` is the right call for a successful POST versus a
  uniform `200 OK`. I can justify `201` (see D9) but I'm not precious about
  it if the reviewer expects otherwise.

## 7. Future considerations: reliability & scalability (out of scope here)

This exercise deliberately left out a real database and retry/backoff logic
(see §1, D8, D11) to avoid over-engineering a take-home. Idempotency keys
(D12) are handled, but only within a single process - taking this from an
exercise to a production gateway would mean revisiting the rest of these
tradeoffs.

### Reliability

- **No retry/backoff around the acquiring bank call.** `HTTPAcquirer`
  (`internal/acquirer/acquirer.go`) makes one attempt with a 5s timeout;
  any failure maps straight to `ErrBankUnavailable` and a `503` to the
  merchant (D8). In production I'd add a bounded retry with backoff, plus a
  circuit breaker so a struggling bank doesn't get hammered by every replica
  retrying at once.
- **The idempotency store (D12) doesn't survive a restart or scale past one
  replica.** It's an in-process map, so a redeploy loses in-flight keys, and
  two replicas behind a load balancer wouldn't see each other's keys at all
  - a retry landing on a different replica than the original request would
  cause a real double-charge, exactly the failure D12 exists to prevent.
  Fixing this needs the same shared external store as the repository fix
  below, with a TTL (Stripe expires idempotency keys after 24h) so the store
  doesn't grow forever.

### Scalability

- **The in-memory repository is the main blocker to scaling out** (the
  idempotency store above has the same problem, for the same reason).
  `PaymentsRepository` (`internal/repository/payments.go`) is a
  mutex-guarded map, private to one process. Running more than one replica
  behind a load balancer - the normal way to add throughput - means each
  replica has its own, inconsistent view of payments: a `GET` could `404`
  depending on which replica handled the earlier `POST`. Fixing this means
  swapping in a shared external store (Postgres, or Redis if eventual
  consistency / read-your-writes on a single key is good enough), keeping
  the same constructor-injection pattern already used for `Acquirer` (D7)
  rather than inventing a speculative `Repository` interface today (D11).
- **Everything else already scales as-is.** Handlers are stateless per
  request, the acquirer client is a plain `http.Client` with no shared
  mutable state, and the bank call already has an explicit timeout so one
  slow call can't tie up a goroutine forever. Once the repository is
  externalized, running N replicas behind a load balancer is just
  infrastructure work, not a code change.
- **The acquiring bank itself becomes the bottleneck at scale**, not the
  gateway - worth tuning connection pooling / keep-alive on the
  `http.Client`, and, per the reliability point above, a circuit breaker so
  replicas fail fast instead of piling up requests against a struggling
  bank.
