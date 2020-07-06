# Pod Lifecycle Logger

## documentation

通过定期轮询k8s中的活跃pod，再轮询k8s的metrics相关endpoint`apis/metrics.k8s.io/v1beta1/namespaces/default/pods`，获取到当前集群中所有活跃pods的CPU和memory使用情况，并通过与上一次记录比较，将更新的内容记录下来。最终持久化到log中。

## dependency

需要k8s具有`metrics.k8s.io`的metrics API

## usage

`--kubeconfig` 指定kubeconfig，无则用`InCluster()`config

`--logdir` 指定log的目录，必须具有写入权限，默认为`/log`，建议将host目录挂volume到`/log`下

## image

`docker pull yzs981130/podlifecyclelogger:version-0.0.3`

