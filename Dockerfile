# syntax=docker/dockerfile:1

FROM golang:1.27-trixie AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
	CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/ ./cmd/server ./cmd/migrate

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/server /out/migrate /
ENV LOG_FORMAT=json
EXPOSE 8080
ENTRYPOINT ["/server"]
