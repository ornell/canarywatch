# ---- Build Stage ----
FROM golang:1.20 AS build

# Set the Current Working Directory inside the container
WORKDIR /app

# Copy everything from the current directory to the PWD(Present Working Directory) inside the container
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o canarywatch .

# ---- Final Stage ----
FROM alpine:latest

# Use an unprivileged user
RUN adduser -S -D -H -h /app appuser
USER appuser

# Copy the binary from build stage
COPY --from=build /app/canarywatch /app/

# Set the binary as the entry point of the container
ENTRYPOINT ["/app/canarywatch"]
