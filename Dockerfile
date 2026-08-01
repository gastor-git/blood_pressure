FROM golang:1.25 AS builder

RUN apt-get update && apt-get install -y tzdata

WORKDIR /build

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN go build -trimpath -tags netgo -ldflags '-s -w -extldflags "-static"' -o blood_pressure

FROM scratch

WORKDIR /app

COPY --from=builder /build/blood_pressure .
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo/

# Непривилегированный пользователь; каталог data/sqlite монтируется с хоста
# и должен быть доступен на запись этому uid.
USER 1000:1000

CMD ["./blood_pressure"]
