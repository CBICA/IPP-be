FROM golang
WORKDIR /opt/ipp-drmaa
COPY go.mod go.sum .
RUN go mod download
COPY IPP-Experiment_Defintions ..
COPY src/main.go .
RUN go build -o /opt/ipp-drmaa/bin/run
ENTRYPOINT [ "/opt/ipp-drmaa/bin/run" ]
