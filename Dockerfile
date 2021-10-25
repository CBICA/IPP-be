FROM drmaa/gridengine

# ENV GOPATH /go
# ENV PATH /usr/local/go/bin:${PATH}:${GOPATH}/bin
ENV PATH /usr/local/go/bin:${PATH}
ENV PATH ${PATH}:/opt/sge/include

RUN yum install -y wget tar git gcc && \
    export VERSION=1.16 OS=linux ARCH=amd64 && \
    wget https://dl.google.com/go/go$VERSION.$OS-$ARCH.tar.gz && \
    tar -C /usr/local -xzvf go$VERSION.$OS-$ARCH.tar.gz && \
    rm go$VERSION.$OS-$ARCH.tar.gz

WORKDIR /go
COPY go.mod go.sum ./
COPY IPP-Experiment_Defintions ../IPP-Experiment_Defintions
RUN go mod download
COPY *.go entrypoint.sh ./
ENTRYPOINT [ "/entrypoint.sh" ]
