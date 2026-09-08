package acquirer

import "context"

type MockAcquirer struct {
	AuthorizeFunc func(ctx context.Context, req BankRequest) (BankResponse, error)

	Calls []BankRequest
}

func (f *MockAcquirer) Authorize(ctx context.Context, req BankRequest) (BankResponse, error) {
	f.Calls = append(f.Calls, req)

	if f.AuthorizeFunc != nil {
		return f.AuthorizeFunc(ctx, req)
	}
	return BankResponse{}, nil
}
