# --- build stage ---------------------------------------------------------
FROM golang:1.25-alpine AS build

WORKDIR /src

# Copy the module files first: this layer is cached until dependencies change,
# so ordinary code edits do not re-download the module graph.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 produces a fully static binary, which is what lets the final
# image be distroless/static (no libc, no shell, tiny attack surface).
# The SQL migrations are compiled in via //go:embed, so nothing else is needed
# at runtime.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# --- runtime stage -------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/server /server

# The host (Render/Fly) overrides PORT; this is just the default.
ENV PORT=8080
EXPOSE 8080

USER nonroot:nonroot
ENTRYPOINT ["/server"]
