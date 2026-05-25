FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod .
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/nerdyrmm-agent ./cmd/agent

FROM alpine:3.21
WORKDIR /app
RUN apk add --no-cache bash ca-certificates
COPY --from=build /out/nerdyrmm-agent /app/nerdyrmm-agent
ENTRYPOINT ["/app/nerdyrmm-agent"]
