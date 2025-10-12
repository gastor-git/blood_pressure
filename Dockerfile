FROM golang:1.25

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN go build -ldflags="-s -w" -a -o blood_pressure main.go

CMD ["./blood_pressure"]
