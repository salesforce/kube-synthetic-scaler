# Build the manager binary
FROM golang AS builder

WORKDIR /workspace
# Copy the Source code
COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o manager main.go

# Build
FROM gcr.io/distroless/base
WORKDIR /
COPY --from=builder /workspace/manager .
ENV PATH /workspace:$PATH
ENTRYPOINT ["/manager"]
