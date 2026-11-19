# sing-geosite

[English](#english) | [简体中文](#简体中文)

## English

A minimal fork of `sing-geosite` that builds only two rule-sets from
[`nekolsd/domain-list-community`](https://github.com/nekolsd/domain-list-community):

| Tag | Description |
| --- | --- |
| `cn@!cn` | Domains in the `cn` list that carry the `!cn` attribute (no mainland-China access point). |
| `geolocation-!cn@cn` | Domains in the `geolocation-!cn` list that carry the `cn` attribute. |

For each tag both a sing-box **source** rule-set (`.json`, `version: 3`) and a
**compiled binary** rule-set (`.srs`) are generated under `rule-set/`.

### Build

```sh
go run .
# Force a rebuild even when the upstream release is unchanged
NO_SKIP=true go run .
```

Output:

```
rule-set/cn@!cn.json
rule-set/cn@!cn.srs
rule-set/geolocation-!cn@cn.json
rule-set/geolocation-!cn@cn.srs
```

The `.srs` files are published to the `rule-set` branch. Only the `main` and
`rule-set` branches are used.

### Data source

- Source: [`nekolsd/domain-list-community`](https://github.com/nekolsd/domain-list-community) (`dlc.dat`)
- Publish target: [`yagh779/sing-geosite`](https://github.com/yagh779/sing-geosite)

## 简体中文

本仓库是 `sing-geosite` 的精简分支，仅从
[`nekolsd/domain-list-community`](https://github.com/nekolsd/domain-list-community)
构建两个规则集：

| 标签 | 说明 |
| --- | --- |
| `cn@!cn` | `cn` 列表中带 `!cn` 属性（无中国大陆访问节点）的域名。 |
| `geolocation-!cn@cn` | `geolocation-!cn` 列表中带 `cn` 属性的域名。 |

每个标签同时生成 sing-box **源**规则集（`.json`，`version: 3`）与**编译后的二进制**
规则集（`.srs`），均位于 `rule-set/` 目录。

### 构建

```sh
go run .
# 即使上游未更新也强制重新构建
NO_SKIP=true go run .
```

输出：

```
rule-set/cn@!cn.json
rule-set/cn@!cn.srs
rule-set/geolocation-!cn@cn.json
rule-set/geolocation-!cn@cn.srs
```

`.srs` 文件会发布到 `rule-set` 分支。仓库仅使用 `main` 与 `rule-set` 两个分支。

### 数据源

- 数据源：[`nekolsd/domain-list-community`](https://github.com/nekolsd/domain-list-community)（`dlc.dat`）
- 发布目标：[`yagh779/sing-geosite`](https://github.com/yagh779/sing-geosite)
