FROM golang
WORKDIR /opt/ipp-drmaa
COPY go.{mod,sum} .
RUN go mod download
COPY *.go .
RUN go build -o /opt/ipp-drmaa/bin/run
ENTRYPOINT [ "/opt/ipp-drmaa/bin/run" ]
