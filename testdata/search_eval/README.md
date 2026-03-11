# 搜索评测测试数据

这组数据用于验证记忆搜索在真实外部嵌入模型下的召回、排序与时间衰减效果。

## 文件说明

- `corpus.jsonl`：测试记忆语料。
- `queries.jsonl`：待执行的搜索查询集合。
- `qrels.jsonl`：查询与期望命中的标注关系。
- `experiment_sets.json`：建议的自动化实验参数组合。

## 设计原则

- 同时覆盖 `summary` 与 `error` 两类记忆。
- 查询全部采用自然语言或真实运维表达，不再依赖测试专用向量标记。
- 同时覆盖自然语言描述、错误复用、模块定位、时效性排序四类查询。
- 刻意加入相近但不完全正确的干扰项，用于观察融合与时间衰减是否合理。
- 时间戳覆盖近 7 天、30 天、90 天及更久的历史，以便验证时效策略。

## 使用建议

- 评测应通过真实外部向量服务执行，并通过 `search-eval --config-path /path/to/config.json` 显式指定包含 `embedding` 配置的文件。
- 如需控制报告输出，可分别使用 `--write-json-report` 与 `--write-markdown-report` 开关按格式启停。
- 导入 `corpus.jsonl` 后，按 `queries.jsonl` 逐条执行搜索。
- 将返回结果与 `qrels.jsonl` 对比，计算 `Recall@5`、`MRR@10`、`nDCG@10`。
- 每轮实验保存 topK 明细，方便复盘未召回与排序靠后的样本。
