package httpapi

import (
	"context"

	"goserver/domain"
)

type Pinger interface {
	Ping(ctx context.Context) error
}

type HealthApi struct {
	DB Pinger
}

//rr:route GET /api/health
func (h *HealthApi) Health(ctx context.Context) (domain.Health, error) {
	if err := h.DB.Ping(ctx); err != nil {
		return domain.Health{}, err
	}
	return domain.Health{Status: "ok"}, nil
}
