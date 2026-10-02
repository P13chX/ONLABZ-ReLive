FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/relive-api .

FROM alpine:3.22
RUN adduser -D -H -u 10001 relive
USER relive
COPY --from=build /out/relive-api /usr/local/bin/relive-api
EXPOSE 8090
ENTRYPOINT ["/usr/local/bin/relive-api"]
