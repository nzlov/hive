# 总结记忆模板

---
type: "summary"
title: "<标题>"
tags: ["<业务标签>", "<重要文件>", "<重要方法>"]
summary: "<简介>"
created_at: "<ISO时间>"
certainty: confirmed
---

## Summary
- 详情: <一句话总结，说明核心结论>

## <具体文件名，如 src/order/service.go>
- 详情: <该文件职责/关键行为>
- 依赖: <相关依赖，可选>

## <具体方法名，如 BuildOrderSnapshot>
- 详情: <该方法做什么、输入输出或关键约束>
- 约束: <边界条件或限制，可选>

## <具体逻辑点，如 支付成功后进入已确认状态>
- 详情: <状态流转/规则/判断逻辑>
- 结论: <可复用结论>

## Details
- 详情: <补充信息，可选>

# 错误记忆模板

---
type: "error"
title: "<标题>"
tags: ["<业务标签>", "<错误类型>", "<相关模块>"]
summary: "<简介>"
created_at: "<ISO时间>"
certainty: confirmed
---

## Summary
- 详情: <一句话描述错误与影响>

## <具体文件名，如 src/pay/retry.go>
- 详情: <错误出现位置与影响范围>

## <具体方法名，如 RetryPayment>
- 详情: <触发路径/触发条件>

## <具体逻辑点，如 连接未归还导致连接池泄漏>
- 根因: <根因说明>
- 修复动作: <如何修复>
- 验证结果: <如何验证修复有效>

## Original Error Code
```text
<原错误代码>
```

## Fixed Code
```text
<修复后代码>
```
