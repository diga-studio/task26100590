# task26100590

培训学时合规判定库（Go），计划与学习证据保存在进程内存，由上层顺序调用。

## 用法

```go
s := compliance.NewStore()
_ = s.RegisterPlan(compliance.Plan{
    EmployeeID: "emp-1",
    DeadlineMs: deadline,
    Modules: []compliance.ModulePlan{
        {ModuleID: "A", RequiredMs: 60_000, QuizPassedAtMs: &quizAt}, // nil 表示未通过
    },
})
_ = s.AppendEvidence("emp-1", compliance.Evidence{ModuleID: "A", StartMs: 0, EndMs: 60_000})
res, _ := s.Query("emp-1", nowMs) // res.Status / res.DeficitMs / res.Modules
```

## 判定模型

- 把时间轴切成基本段（段内覆盖它的模块集合恒定），在
  `源→模块(容量=要求学时)→段(容量=段长)→汇` 的流网络上求最大流（Dinic）。
- 最大可计入学时 = 最大流；缺口 = 总要求 − 最大流。归属不按到达顺序抢占，
  只要存在合法归属满足全部模块即判学时足够。
- 查询只使用 `min(查询时刻, 截止时刻)` 之前的学时与测验通过记录；
  证据可乱序、重复追加，结果与追加的顺序和次数无关。

## 测试
```sh
go test ./...
```

覆盖：非抢占归属（A/B 共享窗口）、共享窗口缺口（A、B、C 各 60 秒）、
重复与乱序补报一致性、跨截止截断与补报、测验规则、随机场景不变量、
满规模性能（16 模块 / 10000 区间 / 90 天，单次查询约 2ms）。
