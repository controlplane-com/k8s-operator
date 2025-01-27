FROM golang:1.23 as builder
COPY . /k8s-operator
ENV CGO_ENABLED=0 GOOS=linux
RUN cd /k8s-operator && go build -o /k8s-operator/operator cmd/main.go

FROM alpine:latest
COPY --from="builder" /k8s-operator/chart/crd /service/chart/crd
COPY --from="builder" /k8s-operator/operator /service/operator
WORKDIR "/service"
ENTRYPOINT ["./operator"]