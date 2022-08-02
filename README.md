sre-circuit-breaker
===
按 Google SRE 算法实现的熔断器。参考自 Kratos 项目。

Istio 有熔断管理。但其熔断方式与Hystrix 类似，将问题后端节点直接移出可用节点集合一段时间（带节点数量保护）。

SRE 的方式是按报错比例拦截请求，尽可能压榨后端服务能力，减少向后端请求压力变化毛刺。
