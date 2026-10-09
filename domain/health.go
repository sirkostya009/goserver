package domain

// Health is what the health route answers with.
//
//ggen:generate
type Health struct {
	Status string `json:"status"`
}
