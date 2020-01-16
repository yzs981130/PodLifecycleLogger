# Pod Lifecycle Logger

## dependency

需要k8s具有`metrics.k8s.io`的metrics API

## usage

`--kubeconfig` 指定kubeconfig，无则用`InCluster()`config

`--logdir` 指定log的目录，必须具有写入权限，默认为`tmp/`

## image

`docker pull yzs981130/podlifecyclelogger:version-0.0.1`
