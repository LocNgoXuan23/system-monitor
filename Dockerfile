# syntax=docker/dockerfile:1
FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# cgo required by go-nvml (NVML itself is dlopen'd at runtime, not linked here)
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /out/monitor ./cmd/web

FROM gcr.io/distroless/base-debian12
COPY --from=build /out/monitor /monitor
EXPOSE 8080
ENTRYPOINT ["/monitor"]
