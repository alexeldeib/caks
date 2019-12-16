FROM golang:1.13.5-buster as builder

ENV GO111MODULE=on
ENV PATH=/go/bin:/usr/local/kubebuilder/bin:$PATH

COPY build.sh .
RUN bash build.sh

FROM debian:buster

RUN mkdir -p /usr/local/kubebuilder/bin
COPY --from=builder /go/bin/kustomize /usr/local/bin/kustomize
COPY --from=builder /go/bin/kind /usr/local/bin/kind
COPY --from=builder /go/bin/kubectl /usr/local/bin/kubectl
COPY --from=builder /usr/local/kubebuilder/bin /usr/local/kubebuilder/bin
COPY --from=builder /go/bin/controller-gen /usr/local/bin/controller-gen
COPY copy.sh /usr/local/bin/copy.sh

ENTRYPOINT [ "bash", "/usr/local/bin/copy.sh" ]
