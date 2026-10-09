package httpapi

//go:generate go tool ggen .
//go:generate go tool rr $GOFILE
//go:generate go run ../../cmd/clientgen -go ../../client/go/client_gen.go -ts ../../client/ts .

//rr:api onerror=@handleError on400=@badRequest on404=@notFound on405=@methodNotAllowed
type Api struct {
	HealthApi
	AuthApi
	KeysApi
	TenantsApi
	UsersApi
	PostsApi
}
