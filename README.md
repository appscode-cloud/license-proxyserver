[![Go Report Card](https://goreportcard.com/badge/go.bytebuilders.dev/license-proxyserver)](https://goreportcard.com/report/go.bytebuilders.dev/license-proxyserver)
[![Build Status](https://github.com/bytebuilders/license-proxyserver/workflows/CI/badge.svg)](https://github.com/bytebuilders/license-proxyserver/actions?workflow=CI)
[![Docker Pulls](https://img.shields.io/docker/pulls/appscode/license-proxyserver.svg)](https://hub.docker.com/r/appscode/license-proxyserver/)
[![Slack](https://shields.io/badge/Join_Slack-salck?color=4A154B&logo=slack)](https://slack.appscode.com)
[![Twitter](https://img.shields.io/twitter/follow/kubeops.svg?style=social&logo=twitter&label=Follow)](https://twitter.com/intent/follow?screen_name=Kubeops)

# license-proxyserver

ACE license-proxyserver is an [extended api server](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/apiserver-aggregation/) that automates license issuing process for all products from AppsCode.

## Deploy into a Kubernetes Cluster

You can deploy `license-proxyserver` using Helm chart found [here](https://github.com/bytebuilders/installer/tree/master/charts/license-proxyserver).

```console
helm repo add appscode https://charts.appscode.com/stable/
helm repo update

helm install license-proxyserver appscode/license-proxyserver
```
