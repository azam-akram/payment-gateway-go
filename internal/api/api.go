package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/acquirer"
	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/repository"
	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/service"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/sync/errgroup"
)

const defaultBankSimulatorURL = "http://localhost:8080"

type Api struct {
	router      *chi.Mux
	paymentsSvc *service.PaymentService
}

func New() *Api {
	a := &Api{}

	repo := repository.NewPaymentsRepository()
	acq := acquirer.NewRetryingAcquirer(acquirer.NewHTTPAcquirer(bankSimulatorURL()), acquirer.DefaultRetryConfig)
	a.paymentsSvc = service.New(repo, acq)

	a.setupRouter()

	return a
}

func bankSimulatorURL() string {
	if url := os.Getenv("BANK_SIMULATOR_URL"); url != "" {
		return url
	}
	return defaultBankSimulatorURL
}

func (a *Api) Run(ctx context.Context, addr string) error {
	httpServer := &http.Server{
		Addr:        addr,
		Handler:     a.router,
		BaseContext: func(_ net.Listener) context.Context { return ctx },
	}

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		<-ctx.Done()
		fmt.Printf("shutting down HTTP server\n")
		return httpServer.Shutdown(ctx)
	})

	g.Go(func() error {
		fmt.Printf("starting HTTP server on %s\n", addr)
		err := httpServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			return err
		}

		return nil
	})

	return g.Wait()
}

func (a *Api) setupRouter() {
	a.router = chi.NewRouter()
	a.router.Use(middleware.Logger)

	a.router.Get("/ping", a.PingHandler())
	a.router.Get("/swagger/*", a.SwaggerHandler())

	a.router.Get("/api/payments/{id}", a.GetPaymentHandler())
	a.router.Post("/api/payments", a.PostPaymentHandler())
}
