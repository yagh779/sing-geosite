# sing-geosite

简体中文 | [English](README.md)

本仓库是 `sing-geosite` 的精简分支，仅从
[`yagh779/domain-list-community`](https://github.com/yagh779/domain-list-community)
发布的精简 `dlc.dat` 构建两个 sing-box 规则集：

| 标签 | 说明 |
| --- | --- |
| `cn@!cn` | `cn` 列表中带 `!cn` 属性（源自中国大陆以外、无中国大陆访问节点）的域名。 |
| `geolocation-!cn@cn` | `geolocation-!cn` 列表中带 `cn` 属性的域名。 |

每个标签同时生成 sing-box **源**规则集（`.json`，`version: 3`）与**编译后的二进制**
规则集（`.srs`），均位于 `rule-set/` 目录。

## 构建

```sh
go run .
# Force a rebuild even when the upstream tag data is unchanged
NO_SKIP=true go run .
```

输出：

```
rule-set/cn@!cn.json
rule-set/cn@!cn.srs
rule-set/geolocation-!cn@cn.json
rule-set/geolocation-!cn@cn.srs
```

## 变化检测

构建前，程序会对上游 `cn@!cn` 与 `geolocation-!cn@cn` 条目计算稳定的指纹，并与
`sync-state.json` 对比：

```json
{
  "cn@!cn": "<sha256>",
  "geolocation-!cn@cn": "<sha256>"
}
```

当两个指纹均未变化时，跳过构建与发布步骤。构建成功后，两个指纹会写回
`sync-state.json` 并提交到 `main`。

## 发布

生成的文件会发布到 `rule-set` 分支。仓库仅使用 `main` 与 `rule-set` 两个分支。

## 数据源

- 数据源：[`yagh779/domain-list-community`](https://github.com/yagh779/domain-list-community)（`release` 分支上的 `dlc.dat`）
- 发布目标：[`yagh779/sing-geosite`](https://github.com/yagh779/sing-geosite)（`rule-set` 分支）

数据源仓库本身是
[`v2fly/domain-list-community`](https://github.com/v2fly/domain-list-community)
的重新打包分支，其中 `@!cn` 覆盖部分来自
[`nekolsd/domain-list-community`](https://github.com/nekolsd/domain-list-community)。
