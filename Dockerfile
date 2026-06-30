FROM golang:1.22 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/alerter ./cmd/alerter

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/alerter /alerter
ENTRYPOINT ["/alerter"]