FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
COPY config ./config
COPY samples ./samples
RUN go test ./... && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/clearledger-bidder ./cmd/bidder

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/clearledger-bidder /app/clearledger-bidder
COPY config/campaigns.sample.json /app/config/campaigns.sample.json
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/clearledger-bidder"]
CMD ["-config", "/app/config/campaigns.sample.json"]
